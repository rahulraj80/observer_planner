//go:build !sbo_omit_org_update

package orgclient

// update.go is the org.update component's half of the org client (lane
// MP-GOV4, T-711): the manifest rail and the artifact download. A build
// with -tags sbo_omit_org_update links update_omitted.go instead, so
// internal/update leaves the binary; the 426 version-floor memory and
// ShareOptions stay core (agenttooold.go).

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
	"github.com/marmutapp/superbased-observer/internal/update"
)

// updateRail names this rail in a cross-rail key-identity refusal, beside
// routingPolicyRail and announcementRail in orgpin.go. One org has ONE
// distribution identity; naming the rail is the one thing an operator needs
// in order to act on a disagreement.
const updateRail = "update"

// resetUpdateCache drops the rail's memory on an enrolment identity change
// (identitycaches.go): the previous org's accepted ordinals, pinned key,
// pending nudges and verified manifest.
func (c *Client) resetUpdateCache() {
	c.updates.mu.Lock()
	defer c.updates.mu.Unlock()
	c.updates.lastAccepted = nil
	c.updates.pending = nil
	c.updates.keyID = ""
	c.updates.manifest, c.updates.artifact = update.Manifest{}, update.Artifact{}
	c.updates.state, c.updates.reason = update.StateIdle, ""
}

// markUpdateChannelPendingAfterRefusal arms a manifest fetch after a 426.
//
// THE STRANDING THIS CLOSES. Under [server].min_agent_version_action =
// "refuse", a node below the floor is answered 426 and NOTHING else happens on
// that cycle: no nudge is read, no manifest is fetched, no update is applied.
// The one mechanism that could lift the node over the floor was gated behind
// the push the floor refuses, so a binary-installed auto-apply node re-POSTed
// forever and never updated itself. That is the opposite of what refusal is
// for.
//
// Marking the channel pending costs one small authenticated GET per push
// cycle on a refused node — and only on a refused node — while the fetch's own
// "already current" check makes it a no-op the moment there is nothing new.
// The server's own half (update_versions on the 426 body) is the better
// signal; this is the half that works even against a server too old to send
// it.
func (c *Client) markUpdateChannelPendingAfterRefusal(ctx context.Context) {
	ch := update.ChannelStable
	if row, err := c.store.LoadUpdateState(ctx); err == nil && update.KnownChannel(row.Channel) {
		ch = row.Channel
	}
	c.updates.mu.Lock()
	defer c.updates.mu.Unlock()
	if c.updates.pending == nil {
		c.updates.pending = map[update.Channel]struct{}{}
	}
	c.updates.pending[ch] = struct{}{}
}

// updateCache is the node's memory of the manifest rail, held on the Client.
//
// It is IN-MEMORY in W2 and that is a deliberate, bounded gap. The durable
// home is agent migration 105's update_state (W3); until it exists, a daemon
// restart resets LastManifestVersion to 0. That is SAFE, not merely tolerable:
// rule 6 (replay) is the only rule that reads it, so a reset can at worst make
// the node re-accept the manifest it already holds. It can never accept an
// OLDER version, because rule 7 compares the manifest's version against the
// INSTALLED binary, which no restart changes.
type updateCache struct {
	mu sync.Mutex
	// lastAccepted is the highest manifest_version accepted, per channel.
	lastAccepted map[string]int64
	// pending is the set of channels the last push acknowledgment reported
	// the server AHEAD on. PushOnce computes it (it is the only place that
	// holds the response body); PushLoop drains it. Keeping the computation
	// and the fetch on opposite sides of this field is what keeps PushOnce
	// free of an outbound call of its own.
	pending map[update.Channel]struct{}
	// manifest is the most recently verified manifest, for `observer update
	// status` and the dashboard card to read.
	manifest update.Manifest
	// artifact is the row selected for this platform on that manifest.
	artifact update.Artifact
	// state is the posture the last verification produced.
	state  update.State
	reason update.Reason
	// keyID is the org distribution key this rail has seen, so a CHANGED key
	// is refused rather than TOFU-accepted on a later fetch.
	keyID string
	// result is the last verification's distro facts (selected distro,
	// transition, epoch), for the apply path.
	result update.VerifyResult
	// distro, when set, makes this node DISTRO-AWARE (modular packaging
	// plan 5.6.1): the fetch negotiates the v2 route first and every
	// verification runs the distro rules against the context it returns.
	// Nil is the pre-modular behaviour exactly (v1 only, no distro rule),
	// which is what an old node does and what the four-cell tests model.
	distro func(ctx context.Context, channel update.Channel) update.DistroContext
	// vendorKeys verifies the v2 release index; nil uses the keys compiled
	// into this binary (update.VendorPublicKeys). Tests set throwaway keys.
	vendorKeys []ed25519.PublicKey
}

// SetUpdateDistroSource makes this client distro-aware: fn returns the
// node's distro context (running distro, policy target, ceiling verdict,
// persisted epoch) for a channel at fetch time. The daemon wires it at start
// (cmd/observer/update_distro.go); it is read on every fetch, so a policy or
// ceiling change takes effect on the next nudge without a restart.
func (c *Client) SetUpdateDistroSource(fn func(ctx context.Context, channel update.Channel) update.DistroContext) {
	if c == nil {
		return
	}
	c.updates.mu.Lock()
	c.updates.distro = fn
	c.updates.mu.Unlock()
}

// setUpdateVendorKeys overrides the release-index verification keys (tests).
func (c *Client) setUpdateVendorKeys(keys []ed25519.PublicKey) {
	c.updates.mu.Lock()
	c.updates.vendorKeys = keys
	c.updates.mu.Unlock()
}

// updateDistroContext returns the distro context for a fetch and whether
// this node is distro-aware at all.
func (c *Client) updateDistroContext(ctx context.Context, channel update.Channel) (update.DistroContext, []ed25519.PublicKey, bool) {
	c.updates.mu.Lock()
	fn, keys := c.updates.distro, c.updates.vendorKeys
	c.updates.mu.Unlock()
	if fn == nil {
		return update.DistroContext{}, nil, false
	}
	dc := fn(ctx, channel)
	dc.Aware = true
	if keys == nil {
		keys = update.VendorPublicKeys()
	}
	return dc, keys, true
}

// FetchOrgUpdateManifest pulls the org's signed update manifest over the
// enrolment credential and verifies it with internal/update's own nine-rule
// table (§3.2), then caches the outcome node-side.
//
// The discipline mirrors FetchOrgAnnouncement by design, down to the refusal
// wording, because it is the same rail shape: a signed document, fetched on
// the push cycle the node ALREADY runs, on the connection it already makes,
// to the server it is already enrolled with. That is the whole reason this
// costs no new egress class (§2.1, acceptance 3) — a new timer or a new host
// would not be.
//
// What is verified, and by whom: EVERY trust decision is made by
// update.Verify, which is pure and lives on the node. This function does
// transport and caching only. In particular the SIGNED bytes decide the
// signing message's manifest_version, so an envelope field an attacker could
// edit independently is never load-bearing.
//
// Nothing is sent about the node: this is a GET, and there is no
// acknowledgment wire. The node's own posture rides the push envelope
// (orgcontract.UpdatePostureRow), composed elsewhere.
//
// Returns (false, nil) when nothing is published, when the cache is already
// current, or when a rule 1-5 failure means the document is IGNORED — notify
// is fail-open (§2.4), so a manifest that cannot be decoded, verified or
// parsed produces no banner and no state change, never an error an operator
// must clear. Returns (true, nil) when a new manifest was accepted or when a
// rule 6-9 outcome changed the posture this node reports (blocked,
// stale_manifest), because those are facts an admin needs on the board.
func (c *Client) FetchOrgUpdateManifest(ctx context.Context, channel update.Channel) (bool, error) {
	enr, err := c.store.LoadEnrolment(ctx)
	if err != nil {
		return false, fmt.Errorf("orgclient.FetchOrgUpdateManifest: enrolment: %w", err)
	}
	if enr == nil {
		return false, ErrNotEnrolled
	}
	if channel == "" {
		channel = update.ChannelStable
	}
	bearer, err := c.bearers.LoadBearer()
	if err != nil {
		return false, fmt.Errorf("orgclient.FetchOrgUpdateManifest: bearer: %w", err)
	}
	// The four-cell negotiation (plan 5.6.1, third-pass M2):
	//   new node + new server -> v2 (this branch);
	//   new node + old server -> v2 is not supported (404 / the SPA page), so
	//     fall through to v1, where verify rule 12 accepts the full build only
	//     for a node that runs full and whose ceiling permits full, and
	//     otherwise records blocked{server_lacks_distro_updates};
	//   old node + new server -> an old node never calls v2: it fetches the v1
	//     route, which still serves only the full build, unchanged;
	//   old node + old server -> today's behaviour.
	dc, vendorKeys, aware := c.updateDistroContext(ctx, channel)
	if aware {
		changed, handled, verr := c.fetchOrgUpdateManifestV2(ctx, enr.OrgServerURL, bearer, channel, dc, vendorKeys)
		if handled {
			return changed, verr
		}
	}
	url := strings.TrimRight(enr.OrgServerURL, "/") + "/api/agent/update/manifest?channel=" + string(channel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := c.httpClient.Do(req)
	c.noteRenewalFromResponse(RenewalPathOther, resp, err)
	if err != nil {
		return false, fmt.Errorf("orgclient.FetchOrgUpdateManifest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if c.classifyRail(updateRail, resp) == railReplyNotSupported {
		// Nothing published — and also what a PRE-FEATURE server answers
		// (a 404, or its SPA page; railreply.go), which is exactly the compat
		// shape acceptance 4 requires: a new agent against an old server
		// stays idle and behaves byte-identically to today.
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("orgclient.FetchOrgUpdateManifest: server returned %d", resp.StatusCode)
	}
	var env update.Envelope
	if err := orgcontract.DecodeCapped(resp.Body, maxOrgDocBytes, &env); err != nil {
		return false, fmt.Errorf("orgclient.FetchOrgUpdateManifest: decode: %w", err)
	}

	// ONE org distribution identity across rails (orgpin.go). A first manifest
	// on a long-enrolled node must present the key the routing or announcement
	// rail already pinned, not merely SOME key — without this, the very first
	// update fetch would be a fresh TOFU a network attacker could win.
	if err := c.checkOrgKeyIdentity(ctx, updateRail, env.KeyID); err != nil {
		return false, fmt.Errorf("orgclient.FetchOrgUpdateManifest: %w", err)
	}
	pinned, lastAccepted, err := c.updatePins(ctx, channel, env.KeyID)
	if err != nil {
		return false, err
	}

	goos, goarch := update.CurrentPlatform()
	res, verr := update.Verify(update.VerifyInput{
		Envelope:                    env,
		PinnedKeyID:                 pinned,
		PinnedPublicKey:             env.KeyID,
		InstalledVersion:            c.agentVersion,
		LastAcceptedManifestVersion: lastAccepted,
		AllowDowngrade:              false,
		Now:                         time.Now().UTC(),
		GOOS:                        goos, GOARCH: goarch,
		// Zero unless this node is distro-aware; then rule 12 applies the
		// new-node / old-server rule to this v1 document.
		Distro: dc,
	})
	if verr != nil {
		return c.recordUpdateOutcome(ctx, channel, env, res, verr)
	}
	return c.recordUpdateOutcome(ctx, channel, env, res, nil)
}

// fetchOrgUpdateManifestV2 is the distro-aware fetch: the v2 document and the
// vendor-signed release index in one response, verified by the same nine
// rules plus the three distro rules. handled is false when the server does
// not serve v2 for this node (a pre-v2 server, or a publication without a v2
// object), so the caller falls back to v1 under rule 12.
func (c *Client) fetchOrgUpdateManifestV2(ctx context.Context, serverURL, bearer string, channel update.Channel,
	dc update.DistroContext, vendorKeys []ed25519.PublicKey,
) (changed, handled bool, err error) {
	url := strings.TrimRight(serverURL, "/") + "/api/agent/update/manifest/v2?channel=" + string(channel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, true, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := c.httpClient.Do(req)
	c.noteRenewalFromResponse(RenewalPathOther, resp, err)
	if err != nil {
		return false, true, fmt.Errorf("orgclient.FetchOrgUpdateManifest: v2: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if c.classifyRail(updateRail, resp) == railReplyNotSupported {
		return false, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, true, fmt.Errorf("orgclient.FetchOrgUpdateManifest: v2: server returned %d", resp.StatusCode)
	}
	var env update.EnvelopeV2
	if err := orgcontract.DecodeCapped(resp.Body, maxOrgDocBytes, &env); err != nil {
		return false, true, fmt.Errorf("orgclient.FetchOrgUpdateManifest: v2 decode: %w", err)
	}
	if err := c.checkOrgKeyIdentity(ctx, updateRail, env.KeyID); err != nil {
		return false, true, fmt.Errorf("orgclient.FetchOrgUpdateManifest: %w", err)
	}
	pinned, lastAccepted, err := c.updatePins(ctx, channel, env.KeyID)
	if err != nil {
		return false, true, err
	}
	goos, goarch := update.CurrentPlatform()
	res, verr := update.Verify(update.VerifyInput{
		Envelope:                    env.Envelope,
		PinnedKeyID:                 pinned,
		PinnedPublicKey:             env.KeyID,
		InstalledVersion:            c.agentVersion,
		LastAcceptedManifestVersion: lastAccepted,
		Now:                         time.Now().UTC(),
		GOOS:                        goos, GOARCH: goarch,
		Schema:          update.SchemaV2,
		ReleaseIndexB64: env.ReleaseIndexB64, ReleaseIndexSig: env.ReleaseIndexSig,
		VendorKeys: vendorKeys,
		Distro:     dc,
	})
	changed, err = c.recordUpdateOutcome(ctx, channel, env.Envelope, res, verr)
	return changed, true, err
}

// updatePins returns the key this rail should verify against and the highest
// manifest_version already accepted on the channel.
//
// The key argument is what the SERVER offered. It becomes the pin only on a
// genuinely first sight; once this rail has seen a key, a CHANGED key is
// refused loudly here — the same class of refusal FetchOrgAnnouncement makes,
// with the same remedy (re-enrol to rotate trust).
func (c *Client) updatePins(ctx context.Context, channel update.Channel, offered string) (pinnedKeyID string, lastAccepted int64, err error) {
	c.updates.mu.Lock()
	defer c.updates.mu.Unlock()
	if c.updates.keyID != "" && c.updates.keyID != offered {
		return "", 0, fmt.Errorf("server update-manifest key CHANGED (pinned %s…, got %s…) - refusing; re-enrol to rotate trust",
			prefix8(c.updates.keyID), prefix8(offered))
	}
	if c.updates.lastAccepted == nil {
		c.updates.lastAccepted = map[string]int64{}
	}
	// Cross-rail pins are authoritative when this rail has none of its own:
	// checkOrgKeyIdentity has already refused a disagreement, so whatever it
	// let through IS the org's one identity.
	pinnedKeyID = c.updates.keyID
	if pinnedKeyID == "" {
		pins, perr := c.loadRailPins(ctx)
		if perr == nil {
			for _, p := range pins {
				pinnedKeyID = p.key
				break
			}
		}
	}
	lastAccepted = c.updates.lastAccepted[string(channel)]
	if lastAccepted == 0 {
		// The DURABLE half of rule 6's replay memory (§3.4's
		// last_manifest_version). Without it a restart reset the counter and
		// the node re-accepted the manifest it already held on every boot —
		// safe, but not the durable protection migration 105 promised. The
		// in-memory value still wins when it is ahead: it is this process's
		// own record of what it accepted this run.
		if row, rerr := c.store.LoadUpdateState(ctx); rerr == nil &&
			(row.Channel == "" || row.Channel == channel) {
			lastAccepted = row.LastManifestVersion
		}
	}
	return pinnedKeyID, lastAccepted, nil
}

// recordUpdateOutcome caches a verification result and reports whether the
// node's posture changed.
//
// Failure direction follows §2.4 exactly: rules 1-5 leave the result in
// StateIdle, which this treats as "ignore the document" — no cache write, no
// posture change, nothing to clear. Rules 6-9 return a real posture
// (blocked{...} / stale_manifest) and those ARE recorded, because they are
// facts the org board must show: "this fleet needs MDM" and "this fleet is
// unfreshened" are answers, not errors.
func (c *Client) recordUpdateOutcome(ctx context.Context, channel update.Channel, env update.Envelope, res update.VerifyResult, verr error) (bool, error) {
	c.updates.mu.Lock()
	if c.updates.lastAccepted == nil {
		c.updates.lastAccepted = map[string]int64{}
	}
	if verr != nil {
		if res.State == update.StateIdle {
			// Fail-OPEN: unreadable, unverifiable or unknown-schema documents
			// change nothing. Logged at debug, not warn — an operator has
			// nothing to do about a document their node correctly ignored.
			c.updates.mu.Unlock()
			c.logger.Debug("org update manifest ignored", "rule", res.Rule, "err", verr)
			return false, nil
		}
		changed := c.updates.state != res.State || c.updates.reason != res.Reason
		c.updates.state, c.updates.reason = res.State, res.Reason
		c.updates.manifest, c.updates.artifact = res.Manifest, res.Artifact
		if c.updates.keyID == "" {
			c.updates.keyID = env.KeyID
		}
		c.updates.mu.Unlock()
		c.logger.Info("org update manifest not applicable", "rule", res.Rule,
			"state", res.State, "reason", res.Reason)
		// A rule 6-9 outcome IS a posture the org board and the node's own
		// card must show, so it is written durably too — but WITHOUT the
		// document: an expired or replayed manifest must not become the
		// bytes a later `observer update apply` runs from.
		c.persistUpdateOutcome(ctx, channel, res, "", verr.Error())
		return changed, nil
	}
	if c.updates.keyID == "" {
		c.updates.keyID = env.KeyID
	}
	if prev := c.updates.lastAccepted[string(channel)]; prev >= res.Manifest.ManifestVersion {
		c.updates.mu.Unlock()
		return false, nil // already current
	}
	c.updates.lastAccepted[string(channel)] = res.Manifest.ManifestVersion
	c.updates.manifest, c.updates.artifact = res.Manifest, res.Artifact
	c.updates.state, c.updates.reason = res.State, res.Reason
	c.updates.result = res
	c.updates.mu.Unlock()
	c.logger.Info("org update manifest accepted",
		"channel", channel, "manifest_version", res.Manifest.ManifestVersion,
		"version", res.Manifest.Version, "state", res.State)
	// The DURABLE half (§3.4). Without this the verified manifest lived in
	// this process's memory only: a notify-only node — the BYO default, and
	// the majority of any fleet — showed `idle` with no target on its Health
	// card, no banner, an idle row on the org board, and
	// `observer update apply` with the daemon stopped refused with "this node
	// holds no accepted manifest". The canonical bytes are the ORG's own
	// signed document coming back, never node data going out.
	c.persistUpdateOutcome(ctx, channel, res, canonicalManifestBytes(env), "manifest verified and accepted")
	return true, nil
}

// canonicalManifestBytes recovers the exact signed bytes from the envelope.
//
// The SIGNED bytes are what a later apply must run from, so the string stored
// is the one the signature covered — never a re-marshal of the decoded struct,
// which any encoder difference would make a different document.
func canonicalManifestBytes(env update.Envelope) string {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(env.ManifestB64))
	if err != nil {
		return ""
	}
	return string(raw)
}

// persistUpdateOutcome writes one verification outcome through the ONE store
// seam (internal/store/update.go).
//
// Best-effort by construction: notify is fail-open (§2.4), so a node that
// cannot write its own state still keeps working and still holds the outcome
// in memory for this process's lifetime. A failure is logged, never returned.
func (c *Client) persistUpdateOutcome(ctx context.Context, channel update.Channel, res update.VerifyResult, manifestJSON, detail string) {
	if c.store == nil || res.Manifest.ManifestVersion <= 0 || !update.KnownState(res.State) {
		return
	}
	if err := c.store.SaveVerifiedManifest(ctx, store.VerifiedManifest{
		Channel:         channel,
		ManifestVersion: res.Manifest.ManifestVersion,
		TargetVersion:   res.Manifest.Version,
		ManifestJSON:    manifestJSON,
		State:           res.State,
		Reason:          res.Reason,
		SeenAt:          time.Now().UTC(),
		Detail:          detail,
	}); err != nil {
		c.logger.Warn("org update manifest could not be recorded on this node", "channel", channel, "err", err)
	}
}

// UpdateStatus is what `observer update status` and the dashboard card read
// from the daemon: the last verified manifest and the posture it produced.
type UpdateStatus struct {
	Channel         update.Channel
	ManifestVersion int64
	Version         string
	State           update.State
	Reason          update.Reason
	Notes           string
	NotesURL        string
	EOSAt           string
	Artifact        update.Artifact
	// Schema is the accepted manifest's schema (v1 or v2). Distro is the
	// distro of the selected artifact (full for v1); DistroTransition and
	// DistroEpoch say whether applying it moves this node to another
	// distro, and the epoch the apply then records (plan 5.6.1).
	Schema           string
	Distro           string
	DistroTransition bool
	DistroEpoch      int64
}

// UpdateStatus returns the cached manifest posture. It reads memory only and
// makes no network call, which is what lets a status surface be honest on an
// air-gapped or offline node.
func (c *Client) UpdateStatus() UpdateStatus {
	c.updates.mu.Lock()
	defer c.updates.mu.Unlock()
	st := UpdateStatus{
		Channel:         c.updates.manifest.Channel,
		ManifestVersion: c.updates.manifest.ManifestVersion,
		Version:         c.updates.manifest.Version,
		State:           c.updates.state,
		Reason:          c.updates.reason,
		Notes:           c.updates.manifest.Notes,
		NotesURL:        c.updates.manifest.NotesURL,
		EOSAt:           c.updates.manifest.EOSAt,
		Artifact:        c.updates.artifact,
		Schema:          c.updates.manifest.Schema,
		Distro:          update.NormalizeDistro(c.updates.artifact.Distro),
		// The transition facts belong to the ACCEPTED manifest only.
		DistroTransition: c.updates.result.DistroTransition && c.updates.result.Manifest.ManifestVersion == c.updates.manifest.ManifestVersion,
		DistroEpoch:      c.updates.manifest.DistroEpoch,
	}
	if st.State == "" {
		st.State = update.StateIdle
	}
	return st
}

// noteUpdateNudge is the receiving half of the push-ack update nudge
// (§3.5; see orgcontract.PushResponse.UpdateVersions).
//
// It decodes the version map from the RAW push response — the same raw-body
// pattern maybeApplyGrantReplacement and maybeKickPolicyResourceFetch use, so
// no generated-client regeneration — and returns the channels whose published
// version is AHEAD of what this node has accepted.
//
// The nudge carries no document and forces nothing: every gate the fetch
// applies (the size cap, the cross-rail key agreement, the nine verify rules)
// is unchanged. And because it only reports channels the server is ahead on,
// a fleet with nothing published makes ZERO extra requests — the steady-state
// property acceptance 3 asserts.
func (c *Client) noteUpdateNudge(body []byte) {
	var ack struct {
		UpdateVersions map[string]int64 `json:"update_versions"`
	}
	if err := json.Unmarshal(body, &ack); err != nil || len(ack.UpdateVersions) == 0 {
		return
	}
	c.updates.mu.Lock()
	defer c.updates.mu.Unlock()
	for name, v := range ack.UpdateVersions {
		ch := update.Channel(name)
		// An unknown channel is IGNORED rather than fetched: the vocabulary is
		// closed, and a future channel reaching an older agent is forward
		// compatibility working, not an error.
		if !update.KnownChannel(ch) {
			continue
		}
		if v <= c.updates.lastAccepted[name] {
			continue
		}
		if c.updates.pending == nil {
			c.updates.pending = map[update.Channel]struct{}{}
		}
		c.updates.pending[ch] = struct{}{}
	}
}

// takePendingUpdateChannels drains the nudge set.
func (c *Client) takePendingUpdateChannels() []update.Channel {
	c.updates.mu.Lock()
	defer c.updates.mu.Unlock()
	if len(c.updates.pending) == 0 {
		return nil
	}
	out := make([]update.Channel, 0, len(c.updates.pending))
	for ch := range c.updates.pending {
		out = append(out, ch)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	c.updates.pending = nil
	return out
}

// FetchPendingUpdateManifests fetches the manifest for every channel the last
// push acknowledgment said the server is ahead on. It is the ONE call
// PushLoop makes, so the "never fetch unless the server is ahead" rule lives
// in one place rather than at each call site — and so a fleet with nothing
// published performs no request here at all.
//
// A failure never propagates: an update fetch must not affect push health.
func (c *Client) FetchPendingUpdateManifests(ctx context.Context) {
	for _, ch := range c.takePendingUpdateChannels() {
		if _, err := c.FetchOrgUpdateManifest(ctx, ch); err != nil && !errors.Is(err, ErrNotEnrolled) {
			c.logger.Warn("org update manifest fetch failed", "channel", ch, "err", err)
		}
	}
}

// DownloadUpdateArtifact streams one artifact from the ORG MIRROR to dst.
//
// This is the ONLY place a node fetches update bytes, and the host is the org
// server it is already enrolled with — never GitHub, never npm, never a CDN
// (ruling R8). That is what makes the design air-gap-NATIVE rather than
// air-gap-capable, and what keeps CLAUDE.md's "no network calls in the
// observer/watcher" claim true with exactly one documented exception host.
//
// maxBytes is a HARD ceiling applied to the stream itself, not a check on a
// Content-Length the server chose: TUF's endless-data defence only works if
// the reader stops, so a response that exceeds it is aborted mid-flight and
// the partial bytes are the caller's to discard.
func (c *Client) DownloadUpdateArtifact(ctx context.Context, version, filename string, dst io.Writer, maxBytes int64) error {
	enr, err := c.store.LoadEnrolment(ctx)
	if err != nil {
		return fmt.Errorf("orgclient.DownloadUpdateArtifact: enrolment: %w", err)
	}
	if enr == nil {
		return ErrNotEnrolled
	}
	// The two path segments are composed by this node from a SIGNED manifest,
	// but they are still checked: a traversal here would turn a manifest into
	// a read of the server's own filesystem.
	if strings.ContainsAny(version, `/\`) || strings.ContainsAny(filename, `/\`) ||
		strings.Contains(version, "..") || strings.Contains(filename, "..") {
		return fmt.Errorf("orgclient.DownloadUpdateArtifact: refusing a path-bearing version/filename")
	}
	bearer, err := c.bearers.LoadBearer()
	if err != nil {
		return fmt.Errorf("orgclient.DownloadUpdateArtifact: bearer: %w", err)
	}
	url := strings.TrimRight(enr.OrgServerURL, "/") +
		"/api/agent/update/artifact/" + neturl.PathEscape(version) + "/" + neturl.PathEscape(filename)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := c.httpClient.Do(req)
	c.noteRenewalFromResponse(RenewalPathOther, resp, err)
	if err != nil {
		return fmt.Errorf("orgclient.DownloadUpdateArtifact: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("orgclient.DownloadUpdateArtifact: server returned %d", resp.StatusCode)
	}
	var reader io.Reader = resp.Body
	if maxBytes > 0 {
		reader = io.LimitReader(resp.Body, maxBytes+1)
	}
	n, err := io.Copy(dst, reader)
	if err != nil {
		return fmt.Errorf("orgclient.DownloadUpdateArtifact: %w", err)
	}
	if maxBytes > 0 && n > maxBytes {
		return fmt.Errorf("orgclient.DownloadUpdateArtifact: the artifact exceeded its declared %d-byte size", maxBytes)
	}
	return nil
}
