package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/accessgrant"
	"github.com/marmutapp/superbased-observer/internal/orgclient"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// fakeRequests is a RequestService that records what it was asked to send.
type fakeRequests struct {
	enrolled  bool
	offline   bool
	sent      []orgcontract.AccessRequestCreate
	queued    []orgcontract.AccessRequestCreate
	withdrawn []int64
}

func (f *fakeRequests) Status(context.Context) (orgclient.EnrolmentState, error) {
	return orgclient.EnrolmentState{Enrolled: f.enrolled, OrgName: "Acme"}, nil
}

func (f *fakeRequests) ListMyOrgRequests(context.Context) ([]orgclient.OrgRequest, error) {
	if f.offline {
		return nil, errors.New("dial tcp: connection refused")
	}
	return []orgclient.OrgRequest{{ID: 4, Kind: "other", Message: "need curl", Status: "open", RequestType: accessgrant.TypeGuardExemption}}, nil
}

func (f *fakeRequests) PrepareAccessRequest(_ context.Context, in orgcontract.AccessRequestCreate, optIn bool) (orgcontract.AccessRequestCreate, error) {
	if in.Context != nil {
		shaped := accessgrant.ShapeContext(*in.Context, false, optIn)
		in.Context = &shaped
	}
	return in, nil
}

func (f *fakeRequests) SendOrQueueAccessRequest(_ context.Context, p orgcontract.AccessRequestCreate) (orgclient.OrgRequest, int64, error) {
	if f.offline {
		f.queued = append(f.queued, p)
		return orgclient.OrgRequest{}, int64(len(f.queued)), nil
	}
	f.sent = append(f.sent, p)
	return orgclient.OrgRequest{ID: 9, Status: "open"}, 0, nil
}

func (f *fakeRequests) WithdrawOrgRequest(_ context.Context, id int64) error {
	f.withdrawn = append(f.withdrawn, id)
	if id == 77 {
		return orgclient.ErrRequestNotOpen
	}
	return nil
}

func (f *fakeRequests) AccessGrantsView(context.Context) (store.AccessGrantsView, bool, error) {
	return store.AccessGrantsView{Version: 3, Grants: []store.AccessGrantView{{
		Grant: orgcontract.AccessGrantWire{ID: 12, Type: accessgrant.TypeGuardExemption, RuleID: "R-171", ExpiresAtNs: time.Now().Add(time.Hour).UnixNano()}, Honoured: true,
	}}}, true, nil
}

func (f *fakeRequests) QueuedAccessRequests(context.Context) ([]store.OutboxRequest, error) {
	return []store.OutboxRequest{}, nil
}

func (f *fakeRequests) CancelQueuedAccessRequest(context.Context, int64) (bool, error) {
	return true, nil
}

func (f *fakeRequests) RequestContextShipsRaw(context.Context) bool { return false }

func (f *fakeRequests) RequestableBudgets() []orgclient.RequestableBudget {
	return []orgclient.RequestableBudget{{BudgetID: "b-u1", Scope: "member:u1", Period: "calendar_month", CapTokens: 1_000_000}}
}

func doRequests(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, armLocalWrite(r))
	return rr
}

func TestRequestsPageSoloNodeSaysEnrolFirst(t *testing.T) {
	s, _ := newTestServer(t)
	rr := doRequests(t, s, http.MethodGet, "/api/requests", "")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"available":false`) {
		t.Fatalf("solo GET: %d %s", rr.Code, rr.Body)
	}
	if rr := doRequests(t, s, http.MethodPost, "/api/requests", `{"request":{"message":"x"}}`); rr.Code != http.StatusConflict {
		t.Fatalf("solo POST: %d %s", rr.Code, rr.Body)
	}
}

func TestRequestsPageListsPreviewsSendsAndQueues(t *testing.T) {
	s, _ := newTestServer(t)
	fake := &fakeRequests{enrolled: true}
	s.opts.Requests = fake

	rr := doRequests(t, s, http.MethodGet, "/api/requests", "")
	var page requestsPageResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if !page.Enrolled || len(page.Requests) != 1 || len(page.Grants.Grants) != 1 || len(page.Types) == 0 {
		t.Fatalf("page: %+v", page)
	}
	// The budget picker's options come from the node's budget rail.
	if len(page.Budgets) != 1 || page.Budgets[0].BudgetID != "b-u1" {
		t.Fatalf("budgets: %+v", page.Budgets)
	}

	body := `{"request":{"request_type":"guard_exemption","message":"deploy needs curl","payload":{"rule_id":"R-171","breadth":"rule"},
		"context":{"rule_id":"R-171","target_excerpt":"curl -T x"},"requested_duration_s":3600},"opt_in_excerpt":false}`
	rr = doRequests(t, s, http.MethodPost, "/api/requests/preview", body)
	var pv requestPreview
	if err := json.Unmarshal(rr.Body.Bytes(), &pv); err != nil || rr.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rr.Code, rr.Body)
	}
	if strings.Contains(pv.JSON, "curl -T") || len(pv.Problems) != 0 {
		t.Fatalf("preview leaked the excerpt without opt-in or has problems: %s %+v", pv.JSON, pv.Problems)
	}

	if rr := doRequests(t, s, http.MethodPost, "/api/requests", body); rr.Code != http.StatusCreated || len(fake.sent) != 1 {
		t.Fatalf("send: %d %s", rr.Code, rr.Body)
	}
	if fake.sent[0].Context.TargetExcerpt != "" {
		t.Fatal("the sent request differs from the preview: excerpt shipped without opt-in")
	}

	// A request the local pre-check refuses never leaves the machine.
	bad := `{"request":{"request_type":"guard_exemption","message":"x","payload":{"breadth":"rule"}}}`
	if rr := doRequests(t, s, http.MethodPost, "/api/requests", bad); rr.Code != http.StatusBadRequest || len(fake.sent) != 1 {
		t.Fatalf("invalid: %d %s", rr.Code, rr.Body)
	}

	fake.offline = true
	if rr := doRequests(t, s, http.MethodPost, "/api/requests", body); rr.Code != http.StatusAccepted || len(fake.queued) != 1 {
		t.Fatalf("offline: %d %s", rr.Code, rr.Body)
	}
	rr = doRequests(t, s, http.MethodGet, "/api/requests", "")
	if !strings.Contains(rr.Body.String(), "could not be reached") {
		t.Fatalf("offline list: %s", rr.Body)
	}

	if rr := doRequests(t, s, http.MethodPost, "/api/requests/4/withdraw", ""); rr.Code != http.StatusNoContent {
		t.Fatalf("withdraw: %d", rr.Code)
	}
	if rr := doRequests(t, s, http.MethodPost, "/api/requests/77/withdraw", ""); rr.Code != http.StatusConflict {
		t.Fatalf("withdraw closed: %d", rr.Code)
	}
}
