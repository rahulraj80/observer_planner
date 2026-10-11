package orgclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// leaveServer is an org server that records POST /api/agent/leave.
func leaveServer(t *testing.T, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/agent/leave" {
			if r.Header.Get("Authorization") != "Bearer bearer-xyz" {
				t.Errorf("leave notice Authorization = %q", r.Header.Get("Authorization"))
			}
			hits.Add(1)
			w.WriteHeader(status)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// TestUnenrolStepTable pins the structured unenrol (T-718): the rows, their
// order, and - one check per row - that after Unenroll the artifact the row
// names is gone. A row added without a check fails here.
func TestUnenrolStepTable(t *testing.T) {
	wantOrder := []string{
		"generation_fence", "enrolment_grant", "org_budget", "org_documents",
		"governance", "org_notice", "credentials", "enrolment",
	}
	plan := UnenrolPlan()
	if len(plan) != len(wantOrder) {
		t.Fatalf("plan has %d rows, want %d", len(plan), len(wantOrder))
	}
	for i, s := range plan {
		if s.ID != wantOrder[i] {
			t.Errorf("row %d = %q, want %q (the order is the contract: fence, enforcement, notify, identity)", i, s.ID, wantOrder[i])
		}
		if s.Conferred == "" || s.Retired == "" || s.Phase == "" {
			t.Errorf("row %q is missing its conferred / retired / phase text", s.ID)
		}
		if strings.ContainsAny(s.Conferred+s.Retired, string([]rune{0x2014, 0x2013})) {
			t.Errorf("row %q uses an em or en dash", s.ID)
		}
	}

	ctx := context.Background()
	srv, hits := leaveServer(t, http.StatusNoContent)
	s := newAgentStore(t)
	bs := &memBearerStore{}
	c := newTestClient(t, s, bs)
	pub := enrolFixture(t, s, bs, srv.URL)
	orgKey := OrgKey(srv.URL, "org-1")
	if _, err := s.BumpEnrolmentGeneration(ctx, orgKey, false); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteEnrolmentGrant(ctx, store.EnrolmentGrant{
		OrgKey: orgKey, Generation: 1, OrgID: "org-1", OrgName: "Acme", OrgServerURL: srv.URL,
		KeyPinSHA256: orgcontract.PublicKeyPinHash(pub), Authority: []string{"enforce.budget"},
		GrantedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour), Signature: "sig", ReceiptHash: "rh",
	}); err != nil {
		t.Fatalf("seed grant: %v", err)
	}
	identity, active, err := CurrentBudgetIdentity(ctx, s)
	if err != nil || !active {
		t.Fatalf("CurrentBudgetIdentity: %v active=%v", err, active)
	}
	if err := s.SaveOrgBudget(ctx, orgcontract.BudgetPolicyDoc{BudgetPolicyBody: orgcontract.BudgetPolicyBody{Version: 1}},
		`"e"`, orgcontract.PublicKeyPinHash(pub), identity); err != nil {
		t.Fatalf("seed org budget: %v", err)
	}
	sidecar := filepath.Join(t.TempDir(), "governance-effective.json")
	if err := os.WriteFile(sidecar, []byte(`{"schema":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c.SetGovernanceSidecarPath(sidecar)

	res, err := c.UnenrollWithReport(ctx)
	if err != nil {
		t.Fatalf("UnenrollWithReport: %v", err)
	}
	if !res.WasEnrolled {
		t.Fatal("WasEnrolled = false")
	}

	checks := map[string]func(t *testing.T){
		"generation_fence": func(t *testing.T) {
			gen, ok, err := s.LoadEnrolmentGeneration(ctx, orgKey)
			if err != nil || !ok || !gen.Tombstoned {
				t.Errorf("generation not tombstoned: %+v ok=%v err=%v", gen, ok, err)
			}
		},
		"enrolment_grant": func(t *testing.T) {
			if _, ok, err := s.LoadEnrolmentGrant(ctx, orgKey); err != nil || ok {
				t.Errorf("grant survived: ok=%v err=%v", ok, err)
			}
		},
		"org_budget": func(t *testing.T) {
			got, err := s.LoadOrgBudget(ctx)
			if err != nil || got.Have {
				t.Errorf("org budget cache survived: have=%v err=%v", got.Have, err)
			}
		},
		"org_documents": func(t *testing.T) {
			for _, r := range res.Steps {
				if r.ID == "org_documents" && (!r.Ran || r.Err != "") {
					t.Errorf("org_documents step = %+v", r)
				}
			}
		},
		"governance": func(t *testing.T) {
			if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
				t.Errorf("governance sidecar survived (stat err=%v)", err)
			}
		},
		"org_notice": func(t *testing.T) {
			if hits.Load() != 1 || !res.OrgNotified {
				t.Errorf("org notice: hits=%d notified=%v notice=%q", hits.Load(), res.OrgNotified, res.OrgNotice)
			}
		},
		"enrolment": func(t *testing.T) {
			if enr, err := s.LoadEnrolment(ctx); err != nil || enr != nil {
				t.Errorf("enrolment row survived: %+v err=%v", enr, err)
			}
		},
		"credentials": func(t *testing.T) {
			if _, err := bs.LoadBearer(); !errors.Is(err, ErrNoSecret) {
				t.Errorf("bearer survived: err=%v", err)
			}
		},
	}
	for _, row := range plan {
		check, ok := checks[row.ID]
		if !ok {
			t.Errorf("row %q has no check", row.ID)
			continue
		}
		t.Run(row.ID, check)
	}
}

// TestUnenrolNoticeNeverBlocks: an org server that predates the route (404)
// or cannot be reached still lets the leave complete, and says why.
func TestUnenrolNoticeNeverBlocks(t *testing.T) {
	ctx := context.Background()
	old, _ := leaveServer(t, http.StatusNotFound)
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	for _, tc := range []struct {
		name, url, want string
	}{
		{"older server", old.URL, "predates"},
		{"unreachable", deadURL, "could not be reached"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newAgentStore(t)
			bs := &memBearerStore{}
			c := newTestClient(t, s, bs)
			enrolFixture(t, s, bs, tc.url)
			res, err := c.UnenrollWithReport(ctx)
			if err != nil {
				t.Fatalf("UnenrollWithReport: %v", err)
			}
			if res.OrgNotified || !strings.Contains(res.OrgNotice, tc.want) {
				t.Errorf("notified=%v notice=%q, want a notice containing %q", res.OrgNotified, res.OrgNotice, tc.want)
			}
			if enr, _ := s.LoadEnrolment(ctx); enr != nil {
				t.Error("the leave did not complete")
			}
		})
	}
}

// TestPushCycleRetiresBudgetRailWhenUnenrolled is root cause 2 of the
// 2026-10-09 report: an `observer unenroll` in another process left the
// daemon's composed budget (and its B-625) in memory, because an idle cycle
// returned before the budget sink ever heard about it.
func TestPushCycleRetiresBudgetRailWhenUnenrolled(t *testing.T) {
	s := newAgentStore(t)
	c := newTestClient(t, s, &memBearerStore{})
	var got []BudgetFetchOutcome
	c.SetBudgetSink(func(o BudgetFetchOutcome) { got = append(got, o) })
	_, err := c.pushCycle(context.Background(), &pushCycleState{}, cycleKind{rails: true})
	if !errors.Is(err, errIdle) {
		t.Fatalf("pushCycle err = %v, want errIdle", err)
	}
	if len(got) != 1 || got[0].State != orgcontract.BudgetFetchNotEnrolled || got[0].HaveBody {
		t.Fatalf("budget sink outcomes = %+v, want one not_enrolled outcome with no body", got)
	}
}

// TestRetireIdentityActsOnlyWhenUnenrolled: the dashboard Leave's in-process
// hook must never retire the budget of a node that is still enrolled.
func TestRetireIdentityActsOnlyWhenUnenrolled(t *testing.T) {
	s := newAgentStore(t)
	bs := &memBearerStore{}
	c := newTestClient(t, s, bs)
	var calls int
	c.SetBudgetSink(func(BudgetFetchOutcome) { calls++ })
	enrolFixture(t, s, bs, "https://org.acme.example")
	c.RetireIdentity(context.Background())
	if calls != 0 {
		t.Fatalf("RetireIdentity on an enrolled node called the sink %d times", calls)
	}
	if err := s.DeleteEnrolment(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.RetireIdentity(context.Background())
	if calls != 1 {
		t.Fatalf("RetireIdentity after a leave called the sink %d times, want 1", calls)
	}
}
