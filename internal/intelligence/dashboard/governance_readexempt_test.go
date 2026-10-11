package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// G11 (docs/plans/cloud-intelligence-parity-design-2026-10-03.md): on a
// managed node the Settings section `cloud` is read-only by default, and the
// guard refused EVERY POST under /api/cloud/, including the network-free
// Preview, which writes nothing and egresses nothing. A read-only section must
// still allow its reads: a READ-SHAPED POST is exempt from read-only (never
// from hidden), while every real write under the section is still refused.
func TestGovernanceReadOnlyExemptsReadShapedPosts(t *testing.T) {
	cases := []struct {
		name           string
		settingsHidden []string
		settingsRO     []string
		path           string
		wantRefusal    string // "" = governance must not refuse
		// managed resolves the posture on an Enterprise-Managed node; on an
		// individual node `intel` is a floor (T-379), so its cases split.
		managed bool
	}{
		{"read-only: preview passes", nil, []string{"cloud"}, "/api/cloud/preview", "", false},
		{"read-only: sync refused", nil, []string{"cloud"}, "/api/cloud/sync", "governance_read_only", false},
		{"read-only: login refused", nil, []string{"cloud"}, "/api/cloud/login", "governance_read_only", false},
		{"read-only: enable refused", nil, []string{"cloud"}, "/api/cloud/enable", "governance_read_only", false},
		{"read-only: consent grant refused", nil, []string{"cloud"}, "/api/cloud/consent/grant", "governance_read_only", false},
		{"read-only: delete-account refused", nil, []string{"cloud"}, "/api/cloud/delete-account", "governance_read_only", false},
		{"hidden: preview still refused", []string{"cloud"}, nil, "/api/cloud/preview", "governance_hidden", false},
		// The org-plane section (design 3.5, D-185): `intel` read-only on a
		// managed node refuses its writes.
		{"intel read-only: accept refused", nil, []string{"intel"}, "/api/intel/accept", "governance_read_only", true},
		{"intel read-only: withdraw refused", nil, []string{"intel"}, "/api/intel/withdraw", "governance_read_only", true},
		{"intel hidden: accept refused", []string{"intel"}, nil, "/api/intel/accept", "governance_hidden", true},
		// T-379 / review M4-07: on an INDIVIDUAL node the consent is the
		// developer's own, so neither lock reaches Accept or Withdraw.
		{"individual, intel read-only: accept passes", nil, []string{"intel"}, "/api/intel/accept", "", false},
		{"individual, intel read-only: withdraw passes", nil, []string{"intel"}, "/api/intel/withdraw", "", false},
		{"individual, intel hidden: accept passes", []string{"intel"}, nil, "/api/intel/accept", "", false},
		{"individual, intel hidden: withdraw passes", []string{"intel"}, nil, "/api/intel/withdraw", "", false},
		// Parity phase 3: Enrich is an action the org's own developer_actions
		// setting decides (orgDecidedSettingsPosts), so a read-only `intel`
		// passes it; a hidden `intel` still refuses it.
		{"intel read-only: enrich passes", nil, []string{"intel"}, "/api/intel/enrich", "", true},
		{"intel hidden: enrich refused", []string{"intel"}, nil, "/api/intel/enrich", "governance_hidden", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newRemoteTestServer(t, Options{
				Governance: governedProviderAs(t, tc.managed, tc.settingsHidden, tc.settingsRO),
			})
			h := s.guardedHandler("127.0.0.1:8081")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, loopbackRequest(http.MethodPost, tc.path))
			var refusal governanceRefusal
			_ = json.Unmarshal(rec.Body.Bytes(), &refusal)
			refused := refusal.Family == "node.governance" || refusal.Error == "governance_read_only" || refusal.Error == "governance_hidden"
			if tc.wantRefusal == "" {
				if refused {
					t.Fatalf("POST %s was refused by governance (%s), want it to pass the guard", tc.path, refusal.Error)
				}
				return
			}
			if refusal.Error != tc.wantRefusal {
				t.Fatalf("POST %s = %d %q, want refusal %q (body %s)", tc.path, rec.Code, refusal.Error, tc.wantRefusal, rec.Body.String())
			}
		})
	}
}
