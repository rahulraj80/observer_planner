//go:build !sbo_omit_cloud_intelligence

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/cloudcontract"
	"github.com/marmutapp/superbased-observer/internal/govern"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

func seedOrgSession(t *testing.T, st *store.Store, id string, last time.Time, n int) {
	t.Helper()
	ctx := context.Background()
	pid, err := st.UpsertProject(ctx, "/org/project", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertSession(ctx, models.Session{ID: id, ProjectID: pid, Tool: "codex", StartedAt: last.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	var acts []models.Action
	for i := 0; i < n; i++ {
		acts = append(acts, models.Action{
			SessionID: id, ProjectID: pid, Timestamp: last.Add(-time.Duration(n-1-i) * time.Minute),
			ActionType: "read_file", Tool: "codex", Target: "x.go", SourceFile: "seed", SourceEventID: fmt.Sprintf("%s-%d", id, i),
		})
	}
	if _, err := st.InsertActions(ctx, acts); err != nil {
		t.Fatal(err)
	}
}

// TestOrgIntelBackgroundSubmission: an individual node queues nothing until
// it accepts; after accepting it queues one node-built ORG-plane envelope per
// quiet org session (labelled with the level its bytes carry: no real prompt
// means structural), whose bytes carry the digests
// queued with them (the two-digest truth on the node).
func TestOrgIntelBackgroundSubmission(t *testing.T) {
	ctx := context.Background()
	f := newOrgIntelFixture(t, "individual", true)
	seedOrgSession(t, f.st, "org-s1", time.Now().UTC().Add(-2*time.Hour), 4)
	live := newLiveOrgEnrichment(f.cfgPath, false)
	src := orgIntelSubmitSource(f.st, live, func() bool { return true }, nil)

	if err := src(ctx); err != nil {
		t.Fatal(err)
	}
	if rows, _ := f.st.OrgIntelSubmissions(ctx, "org-1", "", 0); len(rows) != 0 {
		t.Fatalf("queued before consent: %+v", rows)
	}

	r := httptest.NewRequest("GET", "/api/intel/status", nil)
	st, _ := f.d.Status(r)
	if _, err := f.d.Accept(r, st.DisclosureDigest); err != nil {
		t.Fatal(err)
	}
	live.invalidate()
	if err := src(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := f.st.OrgIntelSubmissions(ctx, "org-1", "", 0)
	if len(rows) != 1 || rows[0].SessionID != "org-s1" || rows[0].EvidenceLevel != "structural" || rows[0].Trigger != store.OrgIntelTriggerBackground {
		t.Fatalf("queued = %+v", rows)
	}
	var env cloudcontract.Envelope
	if err := json.Unmarshal(rows[0].Envelope, &env); err != nil {
		t.Fatal(err)
	}
	if env.Authority.Authority != "org" || cloudcontract.UploadDigest(rows[0].Envelope) != rows[0].UploadDigest {
		t.Fatalf("envelope authority %q, digest match %v", env.Authority.Authority, cloudcontract.UploadDigest(rows[0].Envelope) == rows[0].UploadDigest)
	}
	// A second cycle queues nothing more for the same session.
	if err := src(ctx); err != nil {
		t.Fatal(err)
	}
	if rows, _ := f.st.OrgIntelSubmissions(ctx, "org-1", "", 0); len(rows) != 1 {
		t.Fatalf("re-queued: %d rows", len(rows))
	}
}

// resignOrgIntelTerms replaces the fixture's cached requirement with body,
// signed at version.
func resignOrgIntelTerms(t *testing.T, f *orgIntelFixture, body orgcontract.IntelRequirementBody, version int64) {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	doc, err := orgcontract.SignIntelRequirement(priv, "org-1", "u1", version, body)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.UpsertOrgIntelRequirement(context.Background(), store.OrgIntelRequirementRow{
		OrgID: doc.OrgID, Version: doc.Version, Body: doc.Body, BodyHash: doc.BodyHash, Signature: doc.Signature, ServerPubkey: doc.PublicKey,
	}); err != nil {
		t.Fatal(err)
	}
}

// TestOrgIntelBackgroundSweepFollowsOrgTerms pins parity phase 4 on the node:
// on demand only queues nothing; otherwise the org's quiet window and minimum
// action count pick the sessions (a field the org leaves out keeps the node's
// default).
func TestOrgIntelBackgroundSweepFollowsOrgTerms(t *testing.T) {
	ctx := context.Background()
	f := newOrgIntelFixture(t, orgcontract.TenancyManaged, false)
	now := time.Now().UTC()
	seedOrgSession(t, f.st, "few-actions", now.Add(-2*time.Hour), 4)
	seedOrgSession(t, f.st, "busy-quiet", now.Add(-2*time.Hour), 12)
	seedOrgSession(t, f.st, "busy-recent", now.Add(-40*time.Minute), 12)
	src := orgIntelSubmitSource(f.st, newLiveOrgEnrichment(f.cfgPath, false), func() bool { return true }, nil)

	off := false
	body := f.body
	body.Background = &off
	resignOrgIntelTerms(t, f, body, 5)
	if err := src(ctx); err != nil {
		t.Fatal(err)
	}
	if rows, _ := f.st.OrgIntelSubmissions(ctx, "org-1", "", 0); len(rows) != 0 {
		t.Fatalf("on demand only queued %d rows", len(rows))
	}

	body = f.body
	body.MinActions = 10
	body.QuietMinutes = 60
	resignOrgIntelTerms(t, f, body, 6)
	if err := src(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := f.st.OrgIntelSubmissions(ctx, "org-1", "", 0)
	if len(rows) != 1 || rows[0].SessionID != "busy-quiet" {
		t.Fatalf("queued = %+v, want only the quiet session with enough actions", rows)
	}
}

// TestOrgIntelManagedRailReadout walks the ruling-Q2 table one row per case:
// each managed posture names its reason in plain words.
func TestOrgIntelManagedRailReadout(t *testing.T) {
	applied := govern.Effective{Active: true, State: govern.StateApplied, Managed: true}
	withIntel := applied
	withIntel.Authority = []string{govern.AuthorityExtractIntel}
	raised := withIntel
	raised.Share = map[string]any{intelOrgEnrichmentShareKey: true}
	cases := []struct {
		name   string
		eff    govern.Effective
		local  bool
		reason string
		on     bool
		says   string
	}{
		{"no policy", govern.Effective{State: govern.StateNoPolicy, Managed: true}, false, "no_policy", false, "has not published a machine policy"},
		{"policy not in force", govern.Effective{State: govern.StateGrantExpired, Managed: true}, false, "policy_not_in_force", false, "not in force"},
		{"no authority", applied, false, "no_authority", false, "does not include session enrichment"},
		{"not raised", withIntel, false, "not_raised", false, "has not turned it on"},
		{"raised", raised, false, "on", true, "runs on this machine"},
		{"node opted in", govern.Effective{State: govern.StateNoPolicy, Managed: true}, true, "on", true, "runs on this machine"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := orgIntelManagedRailFor(tc.eff, tc.local, "Acme")
			if got.Reason != tc.reason || got.On != tc.on || !strings.Contains(got.Message, tc.says) || !strings.Contains(got.Message, "Acme") {
				t.Fatalf("got %+v", got)
			}
			if strings.Contains(got.Message, "—") {
				t.Fatalf("em dash in copy: %q", got.Message)
			}
		})
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		seen[tc.reason] = true
	}
	for _, r := range orgIntelRailRules {
		if !seen[r.reason] {
			t.Fatalf("rule %q has no case", r.reason)
		}
	}
}
