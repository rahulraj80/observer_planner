package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/govern"
)

// T-435: a page-access grant shows a hidden tab READ-ONLY. The guard serves
// the tab's reads and refuses its writes as read-only (never as hidden),
// and without the grant the tab stays hidden.
func TestPageGrantShowsAHiddenTabReadOnly(t *testing.T) {
	cases := []struct {
		name    string
		granted []string
		method  string
		path    string
		want    string // "" = governance must not refuse
	}{
		{"no grant: read hidden", nil, http.MethodGet, "/api/routing/shadow", "governance_hidden"},
		{"no grant: write hidden", nil, http.MethodPost, "/api/routing/apply", "governance_hidden"},
		{"granted: read passes", []string{"routing/shadow"}, http.MethodGet, "/api/routing/shadow", ""},
		{"granted: write is read-only", []string{"routing/shadow"}, http.MethodPost, "/api/routing/apply", "governance_read_only"},
		{"a sibling tab is unaffected", []string{"routing/shadow"}, http.MethodGet, "/api/routing/decisions", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := governedProvider(t, []string{"routing/shadow"}, nil, nil, nil)
			gov := func(ctx context.Context) govern.Effective {
				e := base(ctx)
				e.GrantedPages = tc.granted
				return e
			}
			s := newRemoteTestServer(t, Options{Governance: gov})
			rec := httptest.NewRecorder()
			s.guardedHandler("127.0.0.1:8081").ServeHTTP(rec, loopbackRequest(tc.method, tc.path))
			var refusal governanceRefusal
			_ = json.Unmarshal(rec.Body.Bytes(), &refusal)
			if tc.want == "" {
				if refusal.Error == "governance_hidden" || refusal.Error == "governance_read_only" {
					t.Fatalf("%s %s refused by governance (%s)", tc.method, tc.path, refusal.Error)
				}
				return
			}
			if refusal.Error != tc.want {
				t.Fatalf("%s %s = %d %q, want %q (%s)", tc.method, tc.path, rec.Code, refusal.Error, tc.want, rec.Body.String())
			}
		})
	}
}
