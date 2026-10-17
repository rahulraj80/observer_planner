package orgclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestEnrollClearsAgentConfigPolicyOnIdentityChange pins review M3-01: a
// re-enrolment into another org, or as another member, drops the cached org
// AI tool policy and its applied state before the new enrolment is written;
// a re-enrolment as the same org and member keeps them.
func TestEnrollClearsAgentConfigPolicyOnIdentityChange(t *testing.T) {
	cases := []struct {
		name     string
		orgID    string
		userID   string
		wantKept bool
	}{
		{"same org and member", "org-1", "scim-42", true},
		{"another org", "org-2", "scim-42", false},
		{"another member", "org-1", "scim-77", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newAgentStore(t)
			c := newTestClient(t, s, &memBearerStore{})
			ctx := context.Background()
			resp := orgcontract.EnrollResponse{
				Bearer: "bearer-1", BearerExpiresAt: "2026-12-01T00:00:00Z",
				OrgID: "org-1", OrgName: "Acme", UserID: "scim-42", UserEmail: "dev@acme.example", Tenancy: "managed",
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeTestJSON(w, http.StatusOK, resp)
			}))
			defer srv.Close()
			if _, _, err := c.Enroll(ctx, srv.URL, "tok_id.secret"); err != nil {
				t.Fatalf("first Enroll: %v", err)
			}
			if err := s.UpsertOrgAgentConfigPolicy(ctx, store.OrgAgentConfigPolicyRow{
				OrgID: "org-1", Subject: "scim-42", Version: 5, Body: "{}", BodyHash: "h", Signature: "s", ServerPubkey: "k", ReceivedAt: time.Now().UTC(),
			}); err != nil {
				t.Fatal(err)
			}
			if err := s.ReplaceAgentConfigOrgState(ctx, []store.AgentConfigOrgStateRow{{
				SettingID: "codex/model", FileID: "codex.config.user", Tool: "codex", Scope: "user", Mode: "enforced", State: "applied", ObservedAt: time.Now().UTC(),
			}}); err != nil {
				t.Fatal(err)
			}

			resp.OrgID, resp.UserID, resp.Bearer = tc.orgID, tc.userID, "bearer-2"
			if _, _, err := c.Enroll(ctx, srv.URL, "tok_id.secret2"); err != nil {
				t.Fatalf("re-Enroll: %v", err)
			}
			_, cached, err := s.GetOrgAgentConfigPolicy(ctx)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := s.LoadAgentConfigOrgState(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if cached != tc.wantKept || (len(rows) > 0) != tc.wantKept {
				t.Fatalf("after re-enrol: policy cached=%v state rows=%d, want kept=%v", cached, len(rows), tc.wantKept)
			}
			if _, ok, err := c.AgentConfigPolicy(ctx); err != nil || ok != tc.wantKept {
				t.Fatalf("AgentConfigPolicy ok=%v err=%v, want %v", ok, err, tc.wantKept)
			}
		})
	}
}
