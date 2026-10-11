package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

func outboxRow(session, digest, trigger string) OrgIntelSubmission {
	return OrgIntelSubmission{
		OrgID: "org-A", SessionID: session, Purpose: "structural_activity_insights", EvidenceLevel: "titles",
		Trigger: trigger, EvidenceDigest: "e-" + digest, UploadDigest: "u-" + digest, Envelope: []byte(`{"b":1}`),
	}
}

// TestOrgIntelOutboxLifecycle pins the org-plane outbox: enqueue is idempotent
// on the bytes, only pending and due retries are sendable, a terminal state
// clears the held bytes (the size stays for the ledger), and leaving the org
// empties it.
func TestOrgIntelOutboxLifecycle(t *testing.T) {
	ctx := context.Background()
	s := intelTestStore(t)
	enrolForIntel(t, s, orgcontract.TenancyIndividual)
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)

	a, ins, err := s.EnqueueOrgIntelSubmission(ctx, outboxRow("s1", "1", OrgIntelTriggerBackground), now)
	if err != nil || !ins || a.State != OrgIntelOutboxPending || a.Bytes != 7 {
		t.Fatalf("enqueue = %+v %v %v", a, ins, err)
	}
	if again, ins, _ := s.EnqueueOrgIntelSubmission(ctx, outboxRow("s1", "1", OrgIntelTriggerBackground), now); ins || again.ID != a.ID {
		t.Fatalf("re-enqueue inserted=%v id=%d", ins, again.ID)
	}
	b, _, _ := s.EnqueueOrgIntelSubmission(ctx, outboxRow("s2", "2", OrgIntelTriggerDeveloper), now)
	if _, _, err := s.EnqueueOrgIntelSubmission(ctx, OrgIntelSubmission{OrgID: "org-A", SessionID: "x"}, now); err == nil {
		t.Fatal("an empty row was queued")
	}

	if err := s.MarkOrgIntelSubmission(ctx, a.ID, OrgIntelSubmissionUpdate{State: OrgIntelOutboxSubmitted, JobID: 9}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkOrgIntelSubmission(ctx, b.ID, OrgIntelSubmissionUpdate{State: OrgIntelOutboxFailedRetryable, Reason: "not_admitted", NextAt: now.Add(time.Hour)}, now); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.SendableOrgIntelSubmissions(ctx, "org-A", now, 10); len(rows) != 0 {
		t.Fatalf("sendable now = %+v", rows)
	}
	rows, _ := s.SendableOrgIntelSubmissions(ctx, "org-A", now.Add(2*time.Hour), 10)
	if len(rows) != 1 || rows[0].ID != b.ID || len(rows[0].Envelope) == 0 {
		t.Fatalf("sendable later = %+v", rows)
	}
	got, ok, _ := s.GetOrgIntelSubmission(ctx, "org-A", a.ID)
	if !ok || got.State != OrgIntelOutboxSubmitted || len(got.Envelope) != 0 || got.Bytes != 7 || got.JobID != 9 || got.Attempts != 1 {
		t.Fatalf("terminal row = %+v", got)
	}
	if err := s.MarkOrgIntelSubmission(ctx, a.ID, OrgIntelSubmissionUpdate{State: "nonsense"}, now); err == nil {
		t.Fatal("unknown state accepted")
	}
	if list, _ := s.OrgIntelSubmissions(ctx, "org-A", "s2", 0); len(list) != 1 {
		t.Fatalf("per-session ledger = %+v", list)
	}

	if err := s.DeleteEnrolment(ctx); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.OrgIntelSubmissions(ctx, "org-A", "", 0); len(list) != 0 {
		t.Fatalf("outbox survived leave: %+v", list)
	}
}

// TestListOrgIntelSubmitCandidates: only quiet ORG-authority sessions with no
// submission row for the org are candidates.
func TestListOrgIntelSubmitCandidates(t *testing.T) {
	ctx := context.Background()
	s := intelTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	// Captured before enrolment: personal, never a candidate.
	seedOrgCandidate(t, s, "personal-1", now.Add(-3*time.Hour), now.Add(-2*time.Hour), 4)
	enrolForIntel(t, s, orgcontract.TenancyIndividual)
	seedOrgCandidate(t, s, "org-quiet", now.Add(-3*time.Hour), now.Add(-2*time.Hour), 4)
	seedOrgCandidate(t, s, "org-busy", now.Add(-time.Hour), now.Add(-time.Minute), 4)
	seedOrgCandidate(t, s, "org-tiny", now.Add(-3*time.Hour), now.Add(-2*time.Hour), 1)
	seedOrgCandidate(t, s, "org-sent", now.Add(-3*time.Hour), now.Add(-2*time.Hour), 4)
	if auth, ok, _ := s.SessionAuthority(ctx, "org-quiet"); !ok || auth.Authority != "org" {
		t.Fatalf("precondition: org-quiet authority = %+v", auth)
	}
	if _, _, err := s.EnqueueOrgIntelSubmission(ctx, outboxRow("org-sent", "x", OrgIntelTriggerBackground), now); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListOrgIntelSubmitCandidates(ctx, "org-A", now.Add(-24*time.Hour), 30*time.Minute, 3, 10, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SessionID != "org-quiet" {
		t.Fatalf("candidates = %+v", got)
	}
}

func seedOrgCandidate(t *testing.T, s *Store, id string, startedAt, lastActionAt time.Time, actionCount int) {
	t.Helper()
	ctx := context.Background()
	pid, err := s.UpsertProject(ctx, "/org/intel", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSession(ctx, models.Session{ID: id, ProjectID: pid, Tool: "codex", StartedAt: startedAt}); err != nil {
		t.Fatal(err)
	}
	var actions []models.Action
	for i := 0; i < actionCount; i++ {
		actions = append(actions, models.Action{
			SessionID: id, ProjectID: pid, Timestamp: lastActionAt.Add(-time.Duration(actionCount-1-i) * time.Minute),
			ActionType: "tool_call", Tool: "codex", SourceFile: "seed", SourceEventID: fmt.Sprintf("%s-%d", id, i),
		})
	}
	if len(actions) > 0 {
		if _, err := s.InsertActions(ctx, actions); err != nil {
			t.Fatal(err)
		}
	}
}
