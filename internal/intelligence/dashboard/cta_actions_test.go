package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/briefing"
	"github.com/marmutapp/superbased-observer/internal/config"
)

func getActions(t *testing.T, s *Server, path string) actionsRailResponse {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s → %d body=%s", path, rr.Code, rr.Body.String())
	}
	var r actionsRailResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &r); err != nil {
		t.Fatalf("GET %s decode: %v", path, err)
	}
	return r
}

// TestBriefingActionsRail_MixedCards verifies the Pull rail turns the
// routing_shadow card (levered: mode + policy, both actionable on the
// default advise/value config) into actionable rows and the trivial-model
// card into a guidance-only row, with actionable rows sorted first.
func TestBriefingActionsRail_MixedCards(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	seedRoutingShadow(t, st, 4, "fast", 1.0) // → routing_shadow_1
	seedBriefingTurn(t, st, "s1", "claude", "claude-opus-4",
		time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC), 20_000, 8_000, 0, 0) // trivial session

	r := getActions(t, s, "/api/briefing/actions?date=2026-08-01")
	if r.Date != "2026-08-01" || r.Basis != briefing.BasisList {
		t.Fatalf("rail = %+v", r)
	}
	// Expect the two routing levers (mode, policy) + the guidance trivial row.
	var levered, guidance int
	for _, a := range r.Actions {
		if a.Actionable {
			levered++
			if a.Detector != "routing_shadow" || a.Category != "routing" {
				t.Fatalf("levered action = %+v", a)
			}
			if a.RestartRequired != true {
				t.Fatalf("routing lever must be restart_required: %+v", a)
			}
			switch a.ChangeID {
			case "mode":
				if a.ConfigSurface != "routing.mode" || a.From != "advise" || a.To != "enforce" {
					t.Fatalf("mode action = %+v", a)
				}
			case "policy":
				if a.ConfigSurface != "routing.policy" || a.From != "value" || a.To != "fast" {
					t.Fatalf("policy action = %+v", a)
				}
			default:
				t.Fatalf("unexpected change_id %q in %+v", a.ChangeID, a)
			}
			if a.ProjectedMonthlyUSD != 120 {
				t.Fatalf("projected monthly = %v, want 120", a.ProjectedMonthlyUSD)
			}
		} else {
			guidance++
			if a.Detector != "trivial_model_overprovisioning" {
				t.Fatalf("guidance action = %+v", a)
			}
			if a.ChangeID != "" || a.ConfigSurface != "" {
				t.Fatalf("guidance action must carry no lever: %+v", a)
			}
		}
	}
	if levered != 2 || guidance != 1 {
		t.Fatalf("rail = %+v, want 2 levered + 1 guidance", r.Actions)
	}
	// Actionable rows must sort before guidance rows.
	for i, a := range r.Actions {
		if i > 0 && r.Actions[i-1].Actionable && !a.Actionable {
			return // found the actionable→guidance boundary at the right place
		}
	}
	t.Fatalf("rail not sorted actionable-first: %+v", r.Actions)
}

// TestBriefingActionsRail_AllLeversAlreadyActive verifies a levered detector
// whose every lever is already active falls back to a guidance-only row (not
// an actionable one), so the rail never offers a no-op write.
func TestBriefingActionsRail_AllLeversAlreadyActive(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	cfgPath := t.TempDir() + "/config.toml"
	cfg := config.Default()
	cfg.Routing.Mode = "enforce" // mode already enforced
	if err := config.WriteToml(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	s.opts.ConfigPath = cfgPath
	seedRoutingShadow(t, st, 2, "value", 0.5) // policy already == value

	r := getActions(t, s, "/api/briefing/actions?date=2026-08-01")
	for _, a := range r.Actions {
		if a.Actionable {
			t.Fatalf("expected no actionable row when all levers active, got %+v", a)
		}
	}
}

// TestBriefingActionsRail_Empty verifies an empty window returns an empty
// actions array (not null), not an error.
func TestBriefingActionsRail_Empty(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	r := getActions(t, s, "/api/briefing/actions?date=2026-08-01")
	if len(r.Actions) != 0 {
		t.Fatalf("rail = %+v, want zero actions", r)
	}
	if r.Actions == nil {
		t.Fatal("actions must be an empty array, not null")
	}
}

// TestBriefingActionsRail_MethodNotAllowed pins that the rail is a pure read
// surface — anything but GET is refused.
func TestBriefingActionsRail_MethodNotAllowed(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/briefing/actions", strings.NewReader(`{}`))
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST actions → %d, want 405", rr.Code)
	}
}
