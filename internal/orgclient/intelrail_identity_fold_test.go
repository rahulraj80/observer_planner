package orgclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestIntelPageInFlightAcrossSameOrgReEnrolmentIsDiscarded pins review M3-09:
// a result page whose request was signed under one enrolment is discarded
// when, while it was in flight, the machine re-enrolled with the SAME org (as
// another member, or the same member again) or the rail's gate closed. The
// old check compared the org id only and cached the page.
func TestIntelPageInFlightAcrossSameOrgReEnrolmentIsDiscarded(t *testing.T) {
	cases := []struct {
		name     string
		inFlight func(t *testing.T, s *store.Store, bs *memBearerStore, gate *bool)
	}{
		{"another member of the same org", func(t *testing.T, s *store.Store, _ *memBearerStore, _ *bool) {
			reEnrol(t, s, "scim-77", "2026-10-05T09:00:00Z")
		}},
		{"the same member enrolled again", func(t *testing.T, s *store.Store, bs *memBearerStore, _ *bool) {
			reEnrol(t, s, "scim-42", "2026-10-05T09:00:00Z")
			bs.bearer = "bearer-new"
		}},
		{"a new bearer for the same enrolment", func(t *testing.T, _ *store.Store, bs *memBearerStore, _ *bool) {
			bs.bearer = "bearer-new"
		}},
		{"the rail gate closed (consent withdrawn)", func(t *testing.T, _ *store.Store, _ *memBearerStore, gate *bool) {
			*gate = false
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			var (
				s    *store.Store
				bs   *memBearerStore
				gate = true
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				tc.inFlight(t, s, bs, &gate)
				writeTestJSON(w, http.StatusOK, orgcontract.IntelResultsResponse{Results: []orgcontract.IntelResultRow{
					{SessionID: "s1", JobID: "10", Title: "title", GeneratedAt: "2026-10-04T10:00:00Z"},
				}})
			}))
			t.Cleanup(srv.Close)
			s = newAgentStore(t)
			if err := s.WriteEnrolment(ctx, store.Enrolment{
				OrgID: "org-1", OrgName: "Acme", OrgServerURL: srv.URL, UserID: "scim-42",
				EnrolledAt: "2026-10-01T09:00:00Z", BearerKeyID: "test",
			}); err != nil {
				t.Fatal(err)
			}
			bs = &memBearerStore{bearer: "bearer-xyz", key: mustKey(t)}
			c := newTestClient(t, s, bs)
			c.SetIntelRail(func() bool { return gate })
			out, err := c.FetchIntelResults(ctx)
			if err != nil || out.State != IntelFetchIdentityChanged || out.Applied != 0 {
				t.Fatalf("outcome = %+v, %v; want identity_changed with nothing applied", out, err)
			}
			if rows, err := s.OrgIntelResultsForSession(ctx, "s1"); err != nil || len(rows) != 0 {
				t.Fatalf("the in-flight page was cached: %+v, %v", rows, err)
			}
		})
	}
}

func reEnrol(t *testing.T, s *store.Store, user, at string) {
	t.Helper()
	cur, err := s.LoadEnrolment(context.Background())
	if err != nil || cur == nil {
		t.Fatalf("enrolment = %v, %v", cur, err)
	}
	cur.UserID, cur.EnrolledAt = user, at
	if err := s.WriteEnrolment(context.Background(), *cur); err != nil {
		t.Fatal(err)
	}
}
