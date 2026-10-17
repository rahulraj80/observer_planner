package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/policyfam/nodegov"
)

// Per-tab visibility (backlog item 4, T-132), the API half. A route a single
// tab owns EXCLUSIVELY is registered with that tab (SectionX.WithTab), and the
// governance guard refuses it while the org hides the tab, exactly as it
// refuses a hidden page's routes. A route that more than one tab (or another
// page) reads stays page-level: hiding a tab is a view control (D-134), so it
// never takes data away from a tab that is still shown.

// expectedTabRoutes pins every tab-owned route. A route moving to another tab,
// or losing its tab, is a visible change here rather than a silent widening.
var expectedTabRoutes = map[string]string{
	"/api/guard/prompt/status":                "security/prompt-guard",
	"/api/guard/prompt/clear":                 "security/prompt-guard",
	"/api/guard/prompt/allow/":                "security/prompt-guard",
	"/api/guard/prompt/probe":                 "security/prompt-guard",
	"/api/guard/mcp":                          "security/mcp",
	"/api/guard/mcp/approve":                  "security/mcp",
	"/api/guard/mcp/reputation":               "security/mcp",
	"/api/guard/jobs/mcp":                     "security/mcp",
	"/api/jobs/mcp-wrap":                      "security/mcp",
	"/api/jobs/mcp-unwrap":                    "security/mcp",
	"/api/jobs/mcp-relay-enable":              "security/mcp",
	"/api/jobs/mcp-relay-disable":             "security/mcp",
	"/api/jobs/mcp-doctor":                    "security/mcp",
	"/api/guard/test":                         "security/rules",
	"/api/guard/jobs/policy":                  "security/rules",
	"/api/guard/conformance":                  "security/coverage",
	"/api/guard/evidence":                     "security/coverage",
	"/api/guard/evidence/download":            "security/coverage",
	"/api/guard/hook-coverage":                "security/coverage",
	"/api/guard/hook-doctor":                  "security/coverage",
	"/api/guard/hook-probe":                   "security/coverage",
	"/api/experiments":                        "compression/experiments",
	"/api/experiments/report":                 "compression/experiments",
	"/api/experiments/stop":                   "compression/experiments",
	"/api/cache/entry-states":                 "cache/overview",
	"/api/cache/events":                       "cache/events",
	"/api/routing/health":                     "routing/overview",
	"/api/routing/simulate":                   "routing/overview",
	"/api/routing/decisions":                  "routing/decisions",
	"/api/routing/tiers":                      "routing/policy",
	"/api/routing/shadow":                     "routing/shadow",
	"/api/routing/apply":                      "routing/shadow",
	"/api/routing/apply/ledger":               "routing/shadow",
	"/api/routing/apply/revert":               "routing/shadow",
	"/api/agent-config/coverage":              "agent-config/coverage",
	"/api/agent-config/history":               "agent-config/history",
	"/api/agent-config/common":                "agent-config/common",
	"/api/agent-config/common/preview":        "agent-config/common",
	"/api/agent-config/common/save":           "agent-config/common",
	"/api/agent-config/instructions":          "agent-config/instructions",
	"/api/agent-config/instructions/new-path": "agent-config/instructions",
	"/api/agent-config/org":                   "agent-config/organization",
	"/api/agent-config/org/apply":             "agent-config/organization",
	"/api/arena/models":                       "terminals/arena",
	"/api/arena/runs":                         "terminals/arena",
	"/api/arena/runs/{id}":                    "terminals/arena",
	"/api/arena/runs/{id}/action/{cid}":       "terminals/arena",
	"/api/arena/runs/{id}/diff/{cid}":         "terminals/arena",
	"/api/terminal/sandbox/config":            "terminals/settings",
	"/api/terminal/ssh/profiles":              "terminals/settings",
	"/api/terminal/ssh/profiles/test":         "terminals/settings",
	"/api/remote/audit":                       "remote/audit",
	"/api/remote/standing-terminal":           "remote/terminal-control",
	"/api/remote/standing-terminal/mint":      "remote/terminal-control",
	"/api/remote/standing-terminal/revoke":    "remote/terminal-control",
	"/api/remote/standing-revoke-on-takeover": "remote/terminal-control",
	"/api/remote/allow-remote-takeover":       "remote/terminal-control",
}

func TestSectionTabGrammar(t *testing.T) {
	sec := SectionSecurity.WithTab("prompt-guard")
	if nav, sub := sec.Split(); nav != SectionSecurity || sub != "" {
		t.Fatalf("Split = %q,%q", nav, sub)
	}
	if got := sec.Tab(); got != "prompt-guard" {
		t.Fatalf("Tab = %q", got)
	}
	if got := SectionSecurity.Tab(); got != "" {
		t.Fatalf("page Tab = %q", got)
	}
	both := SectionSettings.WithSettings("hooks")
	if nav, sub := both.Split(); nav != SectionSettings || sub != "hooks" || both.Tab() != "" {
		t.Fatalf("settings section split = %q,%q tab %q", nav, sub, both.Tab())
	}
}

func TestTabRoutesArePinned(t *testing.T) {
	s := newRemoteTestServer(t, Options{})
	_, _, sections := s.registerRoutes(nil)
	got := map[string]string{}
	for pattern, sec := range sections {
		tab := sec.Tab()
		if tab == "" {
			continue
		}
		nav, _ := sec.Split()
		id := nodegov.NavTabID(string(nav), tab)
		if !nodegov.IsNavTab(id) {
			t.Errorf("route %q is registered with tab %q, which is not a known tab of %q", pattern, tab, nav)
		}
		got[pattern] = id
	}
	var missing, extra []string
	for p, want := range expectedTabRoutes {
		if got[p] != want {
			missing = append(missing, p+" -> "+want+" (registered: "+got[p]+")")
		}
	}
	for p, id := range got {
		if _, ok := expectedTabRoutes[p]; !ok {
			extra = append(extra, p+" -> "+id)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing)+len(extra) > 0 {
		t.Fatalf("tab route registry drifted.\nexpected but not registered:\n  %s\nregistered but not pinned:\n  %s",
			strings.Join(missing, "\n  "), strings.Join(extra, "\n  "))
	}
}

func TestGovernanceAPIRefusesHiddenTabRoutes(t *testing.T) {
	s := newRemoteTestServer(t, Options{
		Governance: governedProvider(t, []string{"security/prompt-guard", "agent-config/history"}, nil, nil, nil),
	})
	h := s.guardedHandler("127.0.0.1:8081")
	cases := []struct {
		name, method, path string
		refused            bool
		section            string
	}{
		{"hidden tab's own read is 404", http.MethodGet, "/api/guard/prompt/status", true, "security/prompt-guard"},
		{"hidden tab's own write is 404", http.MethodPost, "/api/guard/prompt/clear", true, "security/prompt-guard"},
		{"hidden tab on another page", http.MethodGet, "/api/agent-config/history", true, "agent-config/history"},
		{"a shared page read stays", http.MethodGet, "/api/guard/summary", false, ""},
		{"a sibling tab's route stays", http.MethodGet, "/api/guard/mcp", false, ""},
		{"prompt events also feed session detail, so they stay", http.MethodGet, "/api/guard/prompt/events", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, loopbackRequest(tc.method, tc.path))
			var refusal governanceRefusal
			_ = json.Unmarshal(rec.Body.Bytes(), &refusal)
			isRefusal := rec.Code == http.StatusNotFound && refusal.Error == "governance_hidden"
			if isRefusal != tc.refused {
				t.Fatalf("%s %s = %d %s, refused=%v want %v", tc.method, tc.path, rec.Code, rec.Body.String(), isRefusal, tc.refused)
			}
			if tc.refused && (refusal.Section != tc.section || refusal.OrgName == "") {
				t.Fatalf("refusal must name the tab and the organization, got %+v", refusal)
			}
		})
	}
}

// TestGovernanceResponseCarriesHiddenTabs: GET /api/governance tells the SPA
// which tabs to drop.
func TestGovernanceResponseCarriesHiddenTabs(t *testing.T) {
	s := newRemoteTestServer(t, Options{
		Governance: governedProvider(t, []string{"cache/events"}, nil, nil, nil),
	})
	rec := httptest.NewRecorder()
	s.guardedHandler("127.0.0.1:8081").ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/governance"))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/governance = %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Visibility struct {
			HiddenTabs []string `json:"hidden_tabs"`
		} `json:"visibility"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if strings.Join(body.Visibility.HiddenTabs, ",") != "cache/events" {
		t.Fatalf("hidden_tabs = %v", body.Visibility.HiddenTabs)
	}
}
