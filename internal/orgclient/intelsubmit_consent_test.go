package orgclient

import (
	"context"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/store"
)

func acceptOrgIntel(t *testing.T, s *store.Store) {
	t.Helper()
	if _, err := s.RecordOrgIntelConsent(context.Background(), store.OrgIntelConsentEvent{
		OrgID: "org-1", Event: store.OrgIntelConsentAccept, RequirementVersion: 1,
		EvidenceLevel: "titles", Purposes: []string{"structural_activity_insights"}, DisclosureDigest: "d",
		ConsentMode: "interactive", Actor: "dev",
	}); err != nil {
		t.Fatal(err)
	}
}

// TestIntelWithdrawalStopsQueuedEvidence pins review C156-20: a withdrawal
// cancels every queued row in its own transaction, and a row that slipped in
// afterwards is refused by the send-time consent re-check, never sent.
func TestIntelWithdrawalStopsQueuedEvidence(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFake(t)
	f.body = map[string]any{"outcome": "admitted", "job_id": 7}
	cl, s := evidenceClient(t, f)
	pending := queue(t, s, "s1")
	retry := queue(t, s, "s2")
	if err := s.MarkOrgIntelSubmission(ctx, retry.ID, store.OrgIntelSubmissionUpdate{
		State: store.OrgIntelOutboxFailedRetryable, Reason: "unreachable", NextAt: time.Now().Add(-time.Minute),
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	if _, err := s.RecordOrgIntelConsent(ctx, store.OrgIntelConsentEvent{
		OrgID: "org-1", Event: store.OrgIntelConsentWithdraw, ConsentMode: "interactive", Actor: "dev",
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{pending.ID, retry.ID} {
		got, _, _ := s.GetOrgIntelSubmission(ctx, "org-1", id)
		if got.State != store.OrgIntelOutboxRefused || got.Reason != store.OrgIntelOutboxReasonConsentWithdrawn || len(got.Envelope) != 0 {
			t.Fatalf("row %d after withdrawal = %+v, want refused consent_withdrawn with its bytes cleared", id, got)
		}
	}

	// A row queued after the withdrawal (a racing cycle) never leaves.
	late := queue(t, s, "s3")
	cl.runIntelSubmitCycle(ctx)
	if res, err := cl.SubmitIntelEvidence(ctx, late); err != nil || res.Update.State != store.OrgIntelOutboxRefused {
		t.Fatalf("late send = %+v, %v", res, err)
	}
	if len(f.got) != 0 {
		t.Fatalf("the server received %d submissions after the withdrawal", len(f.got))
	}
}
