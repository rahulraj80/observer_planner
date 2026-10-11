package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// Org display-policy cache seam (agent migration 148; the org's signed
// developer cost-display choice, GET /api/agent/display-policy). ONE OWNER:
// the table is written exclusively here. NODE-LOCAL: received state, never
// pushed — pinned by the privacy sentinel.
//
// Deliberately a mirror of organnounce.go: same single-row cache, same pinned
// key column, same upsert. It additionally records the org the document was
// signed for, so a reader can refuse a row that outlived its enrolment.

// OrgDisplayPolicyRow is the cached, VERIFIED document.
type OrgDisplayPolicyRow struct {
	OrgID        string
	Version      int64
	Body         string
	BodyHash     string
	Signature    string
	ServerPubkey string
	// DeveloperCostDisplay is the decoded, normalized body value: "usd" or
	// "tokens".
	DeveloperCostDisplay string
	ReceivedAt           time.Time
}

// UpsertOrgDisplayPolicy replaces the single-row cache. Callers write only a
// document that verified (orgclient.FetchOrgDisplayPolicy).
func (s *Store) UpsertOrgDisplayPolicy(ctx context.Context, row OrgDisplayPolicyRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO org_display_policy (id, org_id, version, body, body_hash, signature, server_pubkey, developer_cost_display, received_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  org_id = excluded.org_id, version = excluded.version, body = excluded.body,
		  body_hash = excluded.body_hash, signature = excluded.signature,
		  server_pubkey = excluded.server_pubkey,
		  developer_cost_display = excluded.developer_cost_display,
		  received_at = excluded.received_at`,
		row.OrgID, row.Version, row.Body, row.BodyHash, row.Signature, row.ServerPubkey,
		row.DeveloperCostDisplay, timestamp(row.ReceivedAt))
	if err != nil {
		return fmt.Errorf("store.UpsertOrgDisplayPolicy: %w", err)
	}
	return nil
}

// DeveloperCostDisplay is how this node's DEVELOPER-facing surfaces render a
// cost figure (operator ruling 2026-10-02: "Admins can choose whether
// developers see cost or only token usage").
type DeveloperCostDisplay struct {
	// Mode is orgcontract.DisplayCostUSD ("usd") or DisplayCostTokens
	// ("tokens").
	Mode string
	// Source is "org" when the org's verified signed document decided,
	// "default" otherwise.
	Source string
	// Version is the document version when Source is "org".
	Version int64
	// Unverified is true when this node is enrolled but its cached signed
	// policy could not be READ, so it cannot tell what the org chose (Sol
	// A4-03). Mode is still the dollars fallback the text surfaces rely on;
	// a surface that must not fall open to dollars (the node dashboard)
	// checks this instead of trusting that fallback.
	Unverified bool
}

// ResolveDeveloperCostDisplay is THE resolution of the org's developer
// cost-display choice on this node - the one seam behind every developer
// surface: the dashboard (GET /api/cost-display), the statusline (GET
// /api/statusline), the budget-warning service's messages (the prompt-hook
// notice, `observer guard status`, the desktop notification) and the guard's
// budget verdict text.
//
// It reads the node-local cache the push loop wrote after verifying the org's
// signed document (agent migration 148) and requires CURRENT enrolment in the
// org that document was signed for: a departed org never decides how this
// node renders. A cached policy that cannot be READ is reported as Unverified
// (its Mode is the dollars fallback, which the developer surfaces then refuse
// to trust: they render token usage instead, Sol A4-03 class). Every other
// failure degrades to dollars, the default - a display
// preference must never be the reason a surface fails, and failing toward
// dollars never hides a figure nobody asked to hide.
func (s *Store) ResolveDeveloperCostDisplay(ctx context.Context, enrolled bool, enrolledOrgID string) DeveloperCostDisplay {
	def := DeveloperCostDisplay{Mode: orgcontract.DisplayCostUSD, Source: "default"}
	if s == nil || !enrolled || enrolledOrgID == "" {
		return def
	}
	row, ok, err := s.GetOrgDisplayPolicy(ctx)
	if err != nil {
		def.Unverified = true
		return def
	}
	if !ok || row.OrgID != enrolledOrgID {
		return def
	}
	return DeveloperCostDisplay{
		Mode:    orgcontract.NormalizeDisplayCost(row.DeveloperCostDisplay),
		Source:  "org",
		Version: row.Version,
	}
}

// GetOrgDisplayPolicy returns the cached document. ok=false when absent —
// the state of every solo install and of an enrolled node whose server
// predates the rail — which every reader treats as "dollars".
func (s *Store) GetOrgDisplayPolicy(ctx context.Context) (OrgDisplayPolicyRow, bool, error) {
	var (
		row OrgDisplayPolicyRow
		ts  string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT org_id, version, body, body_hash, signature, server_pubkey, developer_cost_display, received_at
		FROM org_display_policy WHERE id = 1`).
		Scan(&row.OrgID, &row.Version, &row.Body, &row.BodyHash, &row.Signature, &row.ServerPubkey,
			&row.DeveloperCostDisplay, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return OrgDisplayPolicyRow{}, false, nil
	}
	if err != nil {
		return OrgDisplayPolicyRow{}, false, fmt.Errorf("store.GetOrgDisplayPolicy: %w", err)
	}
	row.ReceivedAt = parseStamp(ts)
	return row, true, nil
}
