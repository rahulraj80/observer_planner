package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgclient"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestCostDisplay walks every case GET /api/cost-display distinguishes, one
// row each: the org's verified choice applies only to a CURRENTLY enrolled
// node holding a row for THAT org; everything else is dollars.
func TestCostDisplay(t *testing.T) {
	cases := []struct {
		name       string
		enrolment  EnrolmentService // nil = solo install
		seed       *store.OrgDisplayPolicyRow
		wantMode   string
		wantSource string
	}{
		{name: "solo install: dollars", wantMode: "usd", wantSource: "default"},
		{name: "enrolled, nothing fetched yet: dollars", enrolment: enrolledService(), wantMode: "usd", wantSource: "default"},
		{
			name: "enrolled, org chose tokens", enrolment: enrolledService(),
			seed:     &store.OrgDisplayPolicyRow{OrgID: "org-1", Version: 1, DeveloperCostDisplay: "tokens"},
			wantMode: "tokens", wantSource: "org",
		},
		{
			name: "enrolled, org chose dollars", enrolment: enrolledService(),
			seed:     &store.OrgDisplayPolicyRow{OrgID: "org-1", Version: 2, DeveloperCostDisplay: "usd"},
			wantMode: "usd", wantSource: "org",
		},
		{
			name: "row left behind by a different org: dollars", enrolment: enrolledService(),
			seed:     &store.OrgDisplayPolicyRow{OrgID: "org-old", Version: 9, DeveloperCostDisplay: "tokens"},
			wantMode: "usd", wantSource: "default",
		},
		{
			name: "unenrolled with a surviving row: dollars", enrolment: &fakeEnrolment{state: orgclient.EnrolmentState{Enrolled: false}},
			seed:     &store.OrgDisplayPolicyRow{OrgID: "org-1", Version: 1, DeveloperCostDisplay: "tokens"},
			wantMode: "usd", wantSource: "default",
		},
		{
			name: "unknown stored value resolves to token usage", enrolment: enrolledService(),
			seed:     &store.OrgDisplayPolicyRow{OrgID: "org-1", Version: 3, DeveloperCostDisplay: "something-new"},
			wantMode: "tokens", wantSource: "org",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, s := newAnnouncementServerWithOrgEnrolment(t, true, c.enrolment)
			if c.seed != nil {
				row := *c.seed
				row.Body, row.BodyHash, row.Signature, row.ServerPubkey, row.ReceivedAt = "{}", "h", "sig", "pk", time.Now().UTC()
				if err := s.UpsertOrgDisplayPolicy(context.Background(), row); err != nil {
					t.Fatalf("seed: %v", err)
				}
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/cost-display", nil))
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", rr.Code, rr.Body.String())
			}
			var got costDisplayResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Mode != c.wantMode || got.Source != c.wantSource {
				t.Errorf("got %+v, want mode %s source %s", got, c.wantMode, c.wantSource)
			}
			if got.Source == "org" && got.OrgName != "Acme" {
				t.Errorf("org-sourced answer must name the org: %+v", got)
			}
		})
	}
}

// TestCostDisplayUnverifiable (Sol A4-03): when this node cannot tell what
// the org chose (the enrolment read fails, or the cached signed policy cannot
// be read), GET /api/cost-display must not answer with the dollars default as
// if it were authoritative: it answers 503 so the dashboard keeps its
// token-safe view and retries, and the statusline tile reads tokens.
func TestCostDisplayUnverifiable(t *testing.T) {
	t.Run("enrolment status unreadable", func(t *testing.T) {
		h, _ := newAnnouncementServerWithOrgEnrolment(t, true, &fakeEnrolment{statusErr: errors.New("status read failed")})
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/cost-display", nil))
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d (%s), want 503: an unknown policy is not the dollars default", rr.Code, rr.Body.String())
		}
	})
	t.Run("statusline tile fails to tokens", func(t *testing.T) {
		s := &Server{opts: Options{OrgClient: &fakeEnrolment{statusErr: errors.New("status read failed")}}}
		if got := s.statuslineCostDisplay(context.Background()); got != "tokens" {
			t.Fatalf("statusline cost display = %q, want tokens", got)
		}
	})
}
