package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/configapply"
)

// getSuspensions issues GET /api/briefing/suspensions and returns the strip.
func getSuspensions(t *testing.T, s *Server) []configapply.Suspension {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/briefing/suspensions", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("suspensions → %d body=%s", rr.Code, rr.Body.String())
	}
	var out struct {
		Suspensions []configapply.Suspension `json:"suspensions"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Suspensions
}

// briefingHasCard reports whether the composed briefing still carries the
// card with dedupKey.
func briefingHasCard(t *testing.T, s *Server, dedupKey string) bool {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/briefing", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("briefing → %d body=%s", rr.Code, rr.Body.String())
	}
	var b struct {
		Opportunities []struct {
			DedupKey string `json:"dedup_key"`
		} `json:"opportunities"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	for _, o := range b.Opportunities {
		if o.DedupKey == dedupKey {
			return true
		}
	}
	return false
}

// TestBriefingCTASnoozeLifecycle is the snooze arm end-to-end: suspend an
// UN-applied card (time-boxed, no config write) → the card leaves the
// briefing, plan 404s, the strip shows it with its expiry, resume restores
// it (and the plan).
func TestBriefingCTASnoozeLifecycle(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	seedRoutingShadow(t, st, 2, "fast", 1.0)

	if !briefingHasCard(t, s, "routing_shadow_1") {
		t.Fatal("routing_shadow_1 must be visible before suspend")
	}

	rr := postCTA(t, s, "/api/briefing/cta/suspend", `{"dedup_key":"routing_shadow_1","until":"3d"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("suspend → %d body=%s", rr.Code, rr.Body.String())
	}
	var out struct {
		Suspended bool   `json:"suspended"`
		Kind      string `json:"kind"`
		Until     string `json:"until"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Suspended || out.Kind != "snooze" || out.Until == "" {
		t.Fatalf("suspend out = %+v", out)
	}

	if briefingHasCard(t, s, "routing_shadow_1") {
		t.Fatal("routing_shadow_1 must be muted while snoozed")
	}
	if rr := postCTA(t, s, "/api/briefing/cta/plan", `{"dedup_key":"routing_shadow_1"}`); rr.Code != http.StatusNotFound {
		t.Fatalf("plan while snoozed → %d, want 404 (%s)", rr.Code, rr.Body.String())
	}

	susp := getSuspensions(t, s)
	if len(susp) != 1 || susp[0].DedupKey != "routing_shadow_1" || susp[0].Kind != "snooze" || susp[0].Until.IsZero() {
		t.Fatalf("suspensions = %+v", susp)
	}

	rr = postCTA(t, s, "/api/briefing/cta/resume", `{"dedup_key":"routing_shadow_1"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("resume → %d body=%s", rr.Code, rr.Body.String())
	}
	if len(getSuspensions(t, s)) != 0 {
		t.Fatal("suspensions not empty after resume")
	}
	if !briefingHasCard(t, s, "routing_shadow_1") {
		t.Fatal("routing_shadow_1 must return after resume")
	}
	if rr := postCTA(t, s, "/api/briefing/cta/plan", `{"dedup_key":"routing_shadow_1"}`); rr.Code != http.StatusOK {
		t.Fatalf("plan after resume → %d, want 200 (%s)", rr.Code, rr.Body.String())
	}
}

// TestBriefingCTAPauseLifecycle is the pause arm end-to-end: suspend an
// APPLIED card → the lever is restored in the real config (temporary
// revert that KEEPS apply + backup), the card leaves the briefing, the
// strip shows a pause with its resume target; resume re-applies the lever
// as a fresh apply (undo chain intact — revert still works after).
func TestBriefingCTAPauseLifecycle(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.WriteToml(cfgPath, config.Default()); err != nil {
		t.Fatal(err)
	}
	s.opts.ConfigPath = cfgPath
	seedRoutingShadow(t, st, 3, "fast", 1.0)

	if rr := postCTA(t, s, "/api/briefing/cta/apply", `{"dedup_key":"routing_shadow_1","change_id":"policy"}`); rr.Code != http.StatusOK {
		t.Fatalf("apply → %d body=%s", rr.Code, rr.Body.String())
	}
	loadCfg := func() config.Config {
		t.Helper()
		cfg, err := config.Load(config.LoadOptions{GlobalPath: cfgPath})
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	if cfg := loadCfg(); cfg.Routing.Policy != "fast" {
		t.Fatalf("policy after apply = %q, want fast", cfg.Routing.Policy)
	}

	rr := postCTA(t, s, "/api/briefing/cta/suspend", `{"dedup_key":"routing_shadow_1"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("suspend → %d body=%s", rr.Code, rr.Body.String())
	}
	var out struct {
		Suspended bool             `json:"suspended"`
		Kind      string           `json:"kind"`
		Restored  []map[string]any `json:"restored"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Suspended || out.Kind != "pause" || len(out.Restored) != 1 {
		t.Fatalf("suspend out = %+v", out)
	}
	if cfg := loadCfg(); cfg.Routing.Policy != "value" {
		t.Fatalf("policy after pause = %q, want value", cfg.Routing.Policy)
	}

	if briefingHasCard(t, s, "routing_shadow_1") {
		t.Fatal("routing_shadow_1 must be muted while paused")
	}
	susp := getSuspensions(t, s)
	if len(susp) != 1 || susp[0].Kind != "pause" || susp[0].To != "fast" || susp[0].From != "value" {
		t.Fatalf("suspensions = %+v", susp)
	}

	rr = postCTA(t, s, "/api/briefing/cta/resume", `{"dedup_key":"routing_shadow_1"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("resume → %d body=%s", rr.Code, rr.Body.String())
	}
	if len(getSuspensions(t, s)) != 0 {
		t.Fatal("suspensions not empty after resume")
	}
	if cfg := loadCfg(); cfg.Routing.Policy != "fast" {
		t.Fatalf("policy after resume = %q, want fast", cfg.Routing.Policy)
	}
	if !briefingHasCard(t, s, "routing_shadow_1") {
		t.Fatal("routing_shadow_1 must return after resume")
	}

	// The resume re-applied with a fresh stamped snapshot — the undo
	// chain is intact, so a plain revert still restores the pre-apply state.
	rr = postCTA(t, s, "/api/briefing/cta/revert", `{"section":"routing","key":"policy"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("revert after resume → %d body=%s", rr.Code, rr.Body.String())
	}
	if cfg := loadCfg(); cfg.Routing.Policy != "value" {
		t.Fatalf("policy after revert = %q, want value", cfg.Routing.Policy)
	}
}

// TestBriefingCTAPauseResumeByChangeID pauses and resumes a SINGLE lever
// of a multi-lever card, leaving the other applied lever untouched.
func TestBriefingCTAPauseResumeByChangeID(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.WriteToml(cfgPath, config.Default()); err != nil {
		t.Fatal(err)
	}
	s.opts.ConfigPath = cfgPath
	seedRoutingShadow(t, st, 3, "fast", 1.0)
	seedStickinessHolds(t, st, 2)

	if rr := postCTA(t, s, "/api/briefing/cta/apply", `{"dedup_key":"routing_shadow_1","change_id":"mode"}`); rr.Code != http.StatusOK {
		t.Fatalf("apply mode → %d body=%s", rr.Code, rr.Body.String())
	}
	if rr := postCTA(t, s, "/api/briefing/cta/apply", `{"dedup_key":"routing_shadow_1","change_id":"stickiness"}`); rr.Code != http.StatusOK {
		t.Fatalf("apply stickiness → %d body=%s", rr.Code, rr.Body.String())
	}

	rr := postCTA(t, s, "/api/briefing/cta/suspend", `{"dedup_key":"routing_shadow_1","change_id":"mode"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("suspend mode → %d body=%s", rr.Code, rr.Body.String())
	}
	cfg, err := config.Load(config.LoadOptions{GlobalPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Routing.Mode != "advise" {
		t.Fatalf("mode after pause = %q, want advise (restored)", cfg.Routing.Mode)
	}
	if cfg.Routing.Stickiness.MinTurnsBetweenSwitches != 4 {
		t.Fatalf("stickiness after pausing mode = %d, want 4 (untouched)", cfg.Routing.Stickiness.MinTurnsBetweenSwitches)
	}
	susp := getSuspensions(t, s)
	if len(susp) != 1 || susp[0].ChangeID != "mode" {
		t.Fatalf("suspensions = %+v, want just the mode pause", susp)
	}

	rr = postCTA(t, s, "/api/briefing/cta/resume", `{"dedup_key":"routing_shadow_1","change_id":"mode"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("resume mode → %d body=%s", rr.Code, rr.Body.String())
	}
	if len(getSuspensions(t, s)) != 0 {
		t.Fatal("suspensions not empty after resume mode")
	}
	cfg, err = config.Load(config.LoadOptions{GlobalPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Routing.Mode != "enforce" {
		t.Fatalf("mode after resume = %q, want enforce", cfg.Routing.Mode)
	}
}

// TestBriefingCTASuspendRejections pins the bad-body / wrong-method /
// bad-until refusals across the suspend/resume/suspensions surface.
func TestBriefingCTASuspendRejections(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	seedRoutingShadow(t, st, 2, "fast", 1.0)

	cases := []struct {
		name, method, path, body string
		want                     int
	}{
		{"suspend bad body", http.MethodPost, "/api/briefing/cta/suspend", `{not json`, http.StatusBadRequest},
		{"suspend missing dedup_key", http.MethodPost, "/api/briefing/cta/suspend", `{}`, http.StatusBadRequest},
		{"suspend bad until", http.MethodPost, "/api/briefing/cta/suspend", `{"dedup_key":"routing_shadow_1","until":"nope"}`, http.StatusBadRequest},
		{"resume missing dedup_key", http.MethodPost, "/api/briefing/cta/resume", `{}`, http.StatusBadRequest},
		{"suspend wrong method", http.MethodGet, "/api/briefing/cta/suspend", ``, http.StatusMethodNotAllowed},
		{"resume wrong method", http.MethodGet, "/api/briefing/cta/resume", ``, http.StatusMethodNotAllowed},
		{"suspensions wrong method", http.MethodPost, "/api/briefing/suspensions", `{}`, http.StatusMethodNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			s.Handler().ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("%s %s → %d body=%s, want %d", tc.method, tc.path, rr.Code, rr.Body.String(), tc.want)
			}
		})
	}
}
