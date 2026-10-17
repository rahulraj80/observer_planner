package orgclient

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// displayPolicyRail names this rail in cross-rail key-identity refusals.
const displayPolicyRail = "display-policy"

// SetDisplayPolicySink installs the callback run after a NEW display-policy
// version was verified and cached (the push cycle's fetch). Nil-safe; nil
// clears it.
func (c *Client) SetDisplayPolicySink(sink func()) {
	if c == nil {
		return
	}
	c.displayPolicySink = sink
}

// DisplayPolicy returns the last VERIFIED display-policy document this node
// holds for its CURRENT enrolment (ok=false when none, or when the cached row
// was signed for a different org).
func (c *Client) DisplayPolicy(ctx context.Context) (store.OrgDisplayPolicyRow, bool, error) {
	enr, err := c.store.LoadEnrolment(ctx)
	if err != nil {
		return store.OrgDisplayPolicyRow{}, false, fmt.Errorf("orgclient.DisplayPolicy: enrolment: %w", err)
	}
	if enr == nil {
		return store.OrgDisplayPolicyRow{}, false, nil
	}
	row, ok, err := c.store.GetOrgDisplayPolicy(ctx)
	if err != nil || !ok || row.OrgID != enr.OrgID {
		return store.OrgDisplayPolicyRow{}, false, err
	}
	return row, true, nil
}

// FetchOrgDisplayPolicy pulls the org's signed developer cost-display document
// (GET /api/agent/display-policy, operator ruling 2026-10-02: admins always
// see dollars; the admin chooses whether DEVELOPERS' node dashboards show
// dollars or token usage) and caches it node-side after verifying, in order:
//
//   - the document is exactly ONE JSON value within the cap
//     (orgcontract.DecodeCapped);
//   - it was issued for THIS node's org (doc.OrgID == enrolment org, and the
//     org id is bound into the signature);
//   - the offered key agrees with the org identity the other rails and the
//     enrolment already pinned (orgpin.go) — a first fetch cannot TOFU a new
//     key on a long-enrolled node;
//   - the Ed25519 signature verifies over the domain-tagged, org- and
//     version-bound message (orgcontract.VerifyDisplayPolicy).
//
// The STATE RULES are the announcement rail's:
//
//   - 404 (a server that predates the rail) and every other non-200, every
//     transport error and every refused document leave the cached row
//     UNTOUCHED: the node keeps its last verified value, never clears to a
//     default it was not told.
//   - The version is monotonic: a document at or below the cached version is
//     not re-applied, so a captured older document cannot be replayed.
//
// No new network behaviour: it rides the push cycle the node already runs, on
// the connection it already makes, to the server it is already enrolled with.
//
// Returns (true, nil) when a new version was cached.
func (c *Client) FetchOrgDisplayPolicy(ctx context.Context) (bool, error) {
	enr, err := c.store.LoadEnrolment(ctx)
	if err != nil {
		return false, fmt.Errorf("orgclient.FetchOrgDisplayPolicy: enrolment: %w", err)
	}
	if enr == nil {
		return false, ErrNotEnrolled
	}
	bearer, err := c.bearers.LoadBearer()
	if err != nil {
		return false, fmt.Errorf("orgclient.FetchOrgDisplayPolicy: bearer: %w", err)
	}
	url := strings.TrimRight(enr.OrgServerURL, "/") + "/api/agent/display-policy"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := c.httpClient.Do(req)
	c.noteRenewalFromResponse(RenewalPathOther, resp, err)
	if err != nil {
		return false, fmt.Errorf("orgclient.FetchOrgDisplayPolicy: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if c.classifyRail(displayPolicyRail, resp) == railReplyNotSupported {
		// An older server (a 404, or its SPA page served for an unknown GET;
		// railreply.go): keep whatever was last verified, no error.
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("orgclient.FetchOrgDisplayPolicy: server returned %d", resp.StatusCode)
	}
	var doc orgcontract.DisplayPolicyDoc
	if err := orgcontract.DecodeCapped(resp.Body, maxOrgDocBytes, &doc); err != nil {
		return false, fmt.Errorf("orgclient.FetchOrgDisplayPolicy: decode: %w", err)
	}
	if doc.OrgID != enr.OrgID {
		return false, fmt.Errorf("orgclient.FetchOrgDisplayPolicy: document is for org %q, this node is enrolled with %q - refusing", doc.OrgID, enr.OrgID)
	}

	cached, hasCached, err := c.store.GetOrgDisplayPolicy(ctx)
	if err != nil {
		return false, err
	}
	// A row signed for another org (a re-enrol that skipped unenrol's
	// cleanup) constrains nothing about THIS org: treat it as absent.
	if hasCached && cached.OrgID != enr.OrgID {
		hasCached = false
	}
	pinned := doc.PublicKey // first receipt: constrained by checkOrgKeyIdentity below
	repinned := false
	if hasCached {
		if cached.ServerPubkey != doc.PublicKey {
			if err := c.acceptOfferedKeyChange(ctx, displayPolicyRail, cached.ServerPubkey, doc.PublicKey); err != nil {
				return false, fmt.Errorf("orgclient.FetchOrgDisplayPolicy: %w", err)
			}
			repinned = true
		}
		if !repinned {
			pinned = cached.ServerPubkey
		}
	}
	// T-655 (the T-390 shape): verify FIRST, then compare versions. A
	// document's version is only a fact once its signature holds, so a
	// tampered or unsigned document is refused even when it claims a version
	// at or below the cached one, rather than being read as "unchanged".
	if err := c.checkOrgKeyIdentity(ctx, displayPolicyRail, doc.PublicKey); err != nil {
		return false, fmt.Errorf("orgclient.FetchOrgDisplayPolicy: %w", err)
	}
	if err := orgcontract.VerifyDisplayPolicy(doc, enr.OrgID, pinned); err != nil {
		return false, fmt.Errorf("orgclient.FetchOrgDisplayPolicy: %w", err)
	}
	if hasCached && cached.Version >= doc.Version && !(repinned && doc.Version >= cached.Version) {
		return false, nil // already current (or an older document)
	}
	body, err := orgcontract.DecodeDisplayPolicyBody(doc.Body)
	if err != nil {
		return false, fmt.Errorf("orgclient.FetchOrgDisplayPolicy: %w", err)
	}
	if err := c.store.UpsertOrgDisplayPolicy(ctx, store.OrgDisplayPolicyRow{
		OrgID: doc.OrgID, Version: doc.Version, Body: doc.Body, BodyHash: doc.BodyHash,
		Signature: doc.Signature, ServerPubkey: pinned,
		DeveloperCostDisplay: body.DeveloperCostDisplay, ReceivedAt: time.Now().UTC(),
	}); err != nil {
		return false, err
	}
	// Same ordering rule as the announcement rail: the trust root is written
	// only after this document verified and its cache write succeeded.
	if repinned || !hasCached {
		c.adoptEnrolmentKeyMaterial(ctx, pinned)
	}
	c.logger.Info("org display policy cached", "version", doc.Version, "developer_cost_display", body.DeveloperCostDisplay)
	return true, nil
}
