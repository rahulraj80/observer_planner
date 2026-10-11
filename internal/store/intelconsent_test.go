package store

import (
	"context"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

func enrolForIntel(t *testing.T, s *Store, tenancy string) {
	t.Helper()
	if err := s.WriteEnrolment(context.Background(), Enrolment{
		OrgID: "org-A", OrgName: "Acme", OrgServerURL: "https://org.example", UserID: "u1", Tenancy: tenancy,
	}); err != nil {
		t.Fatalf("WriteEnrolment: %v", err)
	}
}

func seedIntelCache(t *testing.T, s *Store, sessionID string, fetched time.Time) {
	t.Helper()
	if err := s.UpsertOrgIntelResult(context.Background(), OrgIntelResult{
		OrgID: "org-A", SessionID: sessionID, JobID: "job-" + sessionID, Title: "t", FetchedAt: fetched,
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
}

func acceptIntel(t *testing.T, s *Store, at time.Time) {
	t.Helper()
	if _, err := s.RecordOrgIntelConsent(context.Background(), OrgIntelConsentEvent{
		OrgID: "org-A", Event: OrgIntelConsentAccept, RequirementVersion: 1, EvidenceLevel: "titles",
		Purposes: []string{"structural_activity_insights"}, DisclosureDigest: "d1", ConsentMode: "interactive", CreatedAt: at,
	}); err != nil {
		t.Fatalf("accept: %v", err)
	}
}

// TestSweepUnconsentedOrgIntelCache pins T-155d: an individual-tenancy node
// drops every cached result it never consented to (all rows with no consent,
// rows fetched before the acceptance otherwise), a managed node keeps its
// cache, and every removal leaves exactly one local receipt.
func TestSweepUnconsentedOrgIntelCache(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		tenancy   string
		acceptAt  *time.Time
		wantGone  int
		wantLeft  int
		wantKind  string
		wantCount int
	}{
		{name: "individual_no_consent", tenancy: orgcontract.TenancyIndividual, wantGone: 2, wantLeft: 0, wantKind: OrgIntelReceiptSweepUnconsented, wantCount: 2},
		{name: "individual_partial", tenancy: orgcontract.TenancyIndividual, acceptAt: ptrTime(base.Add(30 * time.Minute)), wantGone: 1, wantLeft: 1, wantKind: OrgIntelReceiptSweepUnconsented, wantCount: 1},
		{name: "managed_keeps", tenancy: orgcontract.TenancyManaged, wantGone: 0, wantLeft: 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := intelTestStore(t)
			enrolForIntel(t, s, c.tenancy)
			seedIntelCache(t, s, "before", base)
			seedIntelCache(t, s, "after", base.Add(time.Hour))
			if c.acceptAt != nil {
				acceptIntel(t, s, *c.acceptAt)
			}
			n, err := s.SweepUnconsentedOrgIntelCache(ctx, base.Add(2*time.Hour))
			if err != nil {
				t.Fatalf("sweep: %v", err)
			}
			if n != c.wantGone {
				t.Fatalf("removed %d, want %d", n, c.wantGone)
			}
			left, _ := s.OrgIntelCacheCount(ctx)
			if left != c.wantLeft {
				t.Fatalf("left %d, want %d", left, c.wantLeft)
			}
			recs, _ := s.OrgIntelReceipts(ctx, 0)
			if c.wantKind == "" {
				if len(recs) != 0 {
					t.Fatalf("receipts = %+v, want none", recs)
				}
				return
			}
			if len(recs) != 1 || recs[0].Kind != c.wantKind || recs[0].Count != c.wantCount {
				t.Fatalf("receipts = %+v", recs)
			}
			// A second sweep finds nothing and writes no second receipt.
			if n2, _ := s.SweepUnconsentedOrgIntelCache(ctx, base.Add(3*time.Hour)); n2 != 0 {
				t.Fatalf("second sweep removed %d", n2)
			}
			if recs2, _ := s.OrgIntelReceipts(ctx, 0); len(recs2) != 1 {
				t.Fatalf("second sweep wrote a receipt: %+v", recs2)
			}
		})
	}
}

// TestSweepNotEnrolledIsNoop: nothing to sweep without an enrolment.
func TestSweepNotEnrolledIsNoop(t *testing.T) {
	s := intelTestStore(t)
	if n, err := s.SweepUnconsentedOrgIntelCache(context.Background(), time.Now()); err != nil || n != 0 {
		t.Fatalf("sweep = %d, %v", n, err)
	}
}

// TestLeaveEndsOrgIntelConsent: leaving appends a 'leave' event (so a later
// re-enrolment starts with no consent), drops the cached terms and records the
// cache clear.
func TestLeaveEndsOrgIntelConsent(t *testing.T) {
	ctx := context.Background()
	s := intelTestStore(t)
	enrolForIntel(t, s, orgcontract.TenancyIndividual)
	acceptIntel(t, s, time.Now().UTC().Add(-time.Hour))
	seedIntelCache(t, s, "x", time.Now().UTC())
	if err := s.UpsertOrgIntelRequirement(ctx, OrgIntelRequirementRow{OrgID: "org-A", Version: 3, Body: "{}"}); err != nil {
		t.Fatalf("requirement: %v", err)
	}
	if _, ok, _ := s.ActiveOrgIntelConsent(ctx, "org-A"); !ok {
		t.Fatal("no active consent before leave")
	}
	if err := s.DeleteEnrolment(ctx); err != nil {
		t.Fatalf("DeleteEnrolment: %v", err)
	}
	if _, ok, _ := s.ActiveOrgIntelConsent(ctx, "org-A"); ok {
		t.Fatal("consent still active after leave")
	}
	hist, _ := s.OrgIntelConsentHistory(ctx, "org-A", 0)
	if len(hist) != 2 || hist[0].Event != OrgIntelConsentLeave {
		t.Fatalf("history = %+v", hist)
	}
	if _, ok, _ := s.GetOrgIntelRequirement(ctx); ok {
		t.Fatal("requirement survived leave")
	}
	recs, _ := s.OrgIntelReceipts(ctx, 0)
	if len(recs) != 1 || recs[0].Kind != OrgIntelReceiptClearedOnLeave || recs[0].Count != 1 {
		t.Fatalf("receipts = %+v", recs)
	}
}

// TestRecordOrgIntelConsentRejectsBareAccept: an accept without the coverage
// it consents to is refused (INV-13: a consent records what was shown).
func TestRecordOrgIntelConsentRejectsBareAccept(t *testing.T) {
	s := intelTestStore(t)
	for _, ev := range []OrgIntelConsentEvent{
		{OrgID: "org-A", Event: OrgIntelConsentAccept},
		{OrgID: "org-A", Event: OrgIntelConsentAccept, EvidenceLevel: "titles", Purposes: []string{"p"}},
		{OrgID: "", Event: OrgIntelConsentWithdraw},
		{OrgID: "org-A", Event: "grant"},
	} {
		if _, err := s.RecordOrgIntelConsent(context.Background(), ev); err == nil {
			t.Fatalf("accepted %+v", ev)
		}
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

// TestOrgIntelCacheCountIsDistinctSessions pins ruling Q4: the "Results on
// this machine" count is distinct sessions. A session re-enriched (or enriched
// under a second purpose) holds a row per job, and those history rows are the
// org's spend ledger, never extra results.
func TestOrgIntelCacheCountIsDistinctSessions(t *testing.T) {
	ctx := context.Background()
	s := intelTestStore(t)
	at := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)
	for _, r := range []OrgIntelResult{
		{OrgID: "org-A", SessionID: "s-1", JobID: "job-1", Title: "t", FetchedAt: at},
		{OrgID: "org-A", SessionID: "s-1", JobID: "job-2", Title: "t again", FetchedAt: at.Add(time.Minute)},
		{OrgID: "org-A", SessionID: "s-1", JobID: "job-3", Title: "t re-run", FetchedAt: at.Add(2 * time.Minute)},
		{OrgID: "org-A", SessionID: "s-2", JobID: "job-4", Title: "u", FetchedAt: at},
	} {
		if err := s.UpsertOrgIntelResult(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.OrgIntelCacheCount(ctx); err != nil || n != 2 {
		t.Fatalf("results on this machine = %d, %v; want 2 sessions", n, err)
	}
}
