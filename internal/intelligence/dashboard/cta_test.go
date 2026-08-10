package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/configapply"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// seedRoutingShadow seeds advise-mode router decisions under `policy`
// inside the rolling 24h the shadow signal reads (it uses the real
// clock), each rerouting opus → haiku with the given savings. The
// routing_shadow card fires on net savings > 0 regardless of quality
// flags, so this is the whole evidence a CTA plan needs.
func seedRoutingShadow(t *testing.T, st *store.Store, n int, policy string, savings float64) {
	t.Helper()
	now := time.Now().UTC()
	rows := make([]store.RouterDecisionRow, n)
	for i := range rows {
		rows[i] = store.RouterDecisionRow{
			SessionID: "rs" + itoa(i), Timestamp: now.Add(-time.Duration(i) * time.Hour),
			Mode: "advise", Channel: "B",
			OriginalModel: "claude-opus-4-8", SelectedModel: "claude-haiku-4-5",
			TurnKind: "read_only", PolicyName: policy, PolicyHash: "h",
			ReasonCodes: []string{"overpowered_read"}, EstSavingsUSD: savings,
			EstimateVersion: "p1-v1",
		}
	}
	if err := st.InsertRouterDecisions(context.Background(), rows); err != nil {
		t.Fatal(err)
	}
}

// seedStickinessHolds seeds n advise-mode decisions that were held back
// by the coherence floor (reason stickiness_hold) — the evidence behind
// the stickiness lever.
func seedStickinessHolds(t *testing.T, st *store.Store, n int) {
	t.Helper()
	now := time.Now().UTC()
	rows := make([]store.RouterDecisionRow, n)
	for i := range rows {
		rows[i] = store.RouterDecisionRow{
			SessionID: "sh" + itoa(i), Timestamp: now.Add(-time.Duration(i) * time.Hour),
			Mode: "advise", Channel: "B",
			OriginalModel: "claude-opus-4-8", SelectedModel: "claude-opus-4-8",
			TurnKind: "edit", PolicyName: "fast", PolicyHash: "h",
			ReasonCodes: []string{"stickiness_hold"}, EstSavingsUSD: 0.2,
			EstimateVersion: "p1-v1",
		}
	}
	if err := st.InsertRouterDecisions(context.Background(), rows); err != nil {
		t.Fatal(err)
	}
}

// postCTA issues one POST against the server with the given JSON body.
func postCTA(t *testing.T, s *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	s.Handler().ServeHTTP(rr, req)
	return rr
}

func TestBriefingCTAPlanRoutingShadow(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	seedRoutingShadow(t, st, 4, "fast", 1.0)

	rr := postCTA(t, s, "/api/briefing/cta/plan", `{"dedup_key":"routing_shadow_1"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("plan → %d body=%s", rr.Code, rr.Body.String())
	}
	var p ctaPlan
	if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if !p.Appliable || len(p.Changes) == 0 {
		t.Fatalf("plan = %+v, want appliable with changes", p)
	}
	byID := map[string]*configapply.Change{}
	for _, ch := range p.Changes {
		byID[ch.ChangeID] = ch
	}
	mode, ok := byID["mode"]
	if !ok || mode.Section != "routing" || mode.Key != "mode" || mode.To != "enforce" || mode.From != "advise" || !mode.RestartRequired || mode.AlreadyActive {
		t.Fatalf("mode change = %+v, want routing.mode advise→enforce, restart, not already-active", mode)
	}
	pol, ok := byID["policy"]
	if !ok || pol.Section != "routing" || pol.Key != "policy" || pol.To != "fast" || pol.From != "value" || !pol.RestartRequired || pol.AlreadyActive {
		t.Fatalf("policy change = %+v, want routing.policy value→fast, restart, not already-active", pol)
	}
	if _, ok := byID["stickiness"]; ok {
		t.Fatalf("plan = %+v, no stickiness holds seeded — stickiness lever must be absent", p.Changes)
	}
	if p.ProjectedMonthlyUSD != 120 {
		t.Fatalf("projected monthly = %v, want 120 (4×1.00 × 30)", p.ProjectedMonthlyUSD)
	}
	if p.WindowDays != 1 {
		t.Fatalf("plan metadata = %+v", p)
	}
	if p.Evidence["would_reroute"] != 4 {
		t.Fatalf("evidence = %+v", p.Evidence)
	}
}

func TestBriefingCTAPlanStickinessLever(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	// 1 reroute (keeps the card active) + 2 stickiness holds → the
	// stickiness lever must appear with target current−1 (default 5→4).
	seedRoutingShadow(t, st, 1, "fast", 0.5)
	seedStickinessHolds(t, st, 2)

	rr := postCTA(t, s, "/api/briefing/cta/plan", `{"dedup_key":"routing_shadow_1"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("plan → %d body=%s", rr.Code, rr.Body.String())
	}
	var p ctaPlan
	if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if !p.Appliable {
		t.Fatalf("plan = %+v, want appliable", p)
	}
	var stick *configapply.Change
	for _, ch := range p.Changes {
		if ch.ChangeID == "stickiness" {
			stick = ch
		}
	}
	if stick == nil || stick.Key != "stickiness.min_turns_between_switches" ||
		stick.From != "5" || stick.To != "4" || !stick.RestartRequired || stick.AlreadyActive {
		t.Fatalf("stickiness change = %+v, want 5→4, restart, not already-active", stick)
	}
	if p.Evidence["stickiness_holds"] != 2 {
		t.Fatalf("evidence = %+v, want stickiness_holds 2", p.Evidence)
	}
}

func TestBriefingCTAPlanAlreadyActive(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	// Shadow policy == the default on-disk policy (value) → the policy
	// lever is a no-op and must read as already_active; the mode lever
	// (advise→enforce) is still appliable.
	seedRoutingShadow(t, st, 2, "value", 0.5)

	rr := postCTA(t, s, "/api/briefing/cta/plan", `{"dedup_key":"routing_shadow_1"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("plan → %d body=%s", rr.Code, rr.Body.String())
	}
	var p ctaPlan
	if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if !p.Appliable {
		t.Fatalf("plan = %+v, want appliable (mode lever)", p)
	}
	var pol *configapply.Change
	for _, ch := range p.Changes {
		if ch.ChangeID == "policy" {
			pol = ch
		}
	}
	if pol == nil || !pol.AlreadyActive || pol.From != "value" || pol.To != "value" {
		t.Fatalf("policy change = %+v, want value→value already_active", pol)
	}
}

func TestBriefingCTAPlanAllLeversAlreadyActive(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	cfg := config.Default()
	cfg.Routing.Mode = "enforce" // mode already enforced
	if err := config.WriteToml(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	s.opts.ConfigPath = cfgPath
	seedRoutingShadow(t, st, 2, "value", 0.5) // policy already == value

	rr := postCTA(t, s, "/api/briefing/cta/plan", `{"dedup_key":"routing_shadow_1"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("plan → %d body=%s", rr.Code, rr.Body.String())
	}
	var p ctaPlan
	if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Appliable || len(p.Changes) == 0 {
		t.Fatalf("plan = %+v, want not appliable with every lever already_active", p)
	}
	for _, ch := range p.Changes {
		if !ch.AlreadyActive {
			t.Fatalf("change %+v must be already_active", ch)
		}
	}
}

func TestBriefingCTAPlanNoLever(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	// A trivial opus session → trivial_model card (behavioral: no lever).
	seedBriefingTurn(t, st, "s1", "claude", "claude-opus-4",
		time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC), 20_000, 8_000, 0, 0)

	rr := postCTA(t, s, "/api/briefing/cta/plan", `{"dedup_key":"trivial_model_s1"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("plan → %d body=%s", rr.Code, rr.Body.String())
	}
	var p ctaPlan
	if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Appliable || len(p.Changes) != 0 {
		t.Fatalf("plan = %+v, want not appliable with zero changes", p)
	}
	if !strings.Contains(p.Note, "no config lever") {
		t.Fatalf("note = %q, want the no-lever explanation", p.Note)
	}
}

func TestBriefingCTAPlanNotFound(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	rr := postCTA(t, s, "/api/briefing/cta/plan", `{"dedup_key":"nope"}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown card → %d, want 404", rr.Code)
	}
}

func TestBriefingCTAPlanBadBodyAndMethod(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, _ := newBriefingServer(t, e)
	if rr := postCTA(t, s, "/api/briefing/cta/plan", `{not json`); rr.Code != http.StatusBadRequest {
		t.Fatalf("bad body → %d, want 400", rr.Code)
	}
	if rr := postCTA(t, s, "/api/briefing/cta/plan", `{}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("missing dedup_key → %d, want 400", rr.Code)
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/briefing/cta/plan", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET plan → %d, want 405", rr.Code)
	}
}

// TestBriefingCTAApplyWritesLedgersReverts is the full simulate-first
// cycle: plan → apply writes [routing].policy=fast into the real config
// file under a stamped backup + ledger, then revert {routing,policy}
// restores the pre-write state exactly.
func TestBriefingCTAApplyWritesLedgersReverts(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.WriteToml(cfgPath, config.Default()); err != nil {
		t.Fatal(err)
	}
	s.opts.ConfigPath = cfgPath
	seedRoutingShadow(t, st, 3, "fast", 1.0)

	rr := postCTA(t, s, "/api/briefing/cta/apply", `{"dedup_key":"routing_shadow_1","change_id":"policy"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("apply → %d body=%s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["applied"] != true || out["restart_required"] != true || out["backup"] == nil {
		t.Fatalf("apply out = %+v", out)
	}
	cfg, err := config.Load(config.LoadOptions{GlobalPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Routing.Policy != "fast" {
		t.Fatalf("policy after apply = %q, want fast", cfg.Routing.Policy)
	}
	// The stamped pre-write snapshot must exist next to the config file.
	backup := out["backup"].(string)
	if _, err := os.ReadFile(backup); err != nil {
		t.Fatalf("stamped backup missing: %v", err)
	}

	// Ledger records the apply (View-class read).
	rr2 := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/briefing/cta/ledger", nil)
	s.Handler().ServeHTTP(rr2, req)
	if rr2.Code != http.StatusOK {
		t.Fatalf("ledger → %d body=%s", rr2.Code, rr2.Body.String())
	}
	var lr struct {
		Events []struct {
			Kind, Section, Key, To string
		} `json:"events"`
	}
	if err := json.Unmarshal(rr2.Body.Bytes(), &lr); err != nil {
		t.Fatal(err)
	}
	if len(lr.Events) != 1 || lr.Events[0].Kind != "apply" || lr.Events[0].Section != "routing" ||
		lr.Events[0].Key != "policy" || lr.Events[0].To != "fast" {
		t.Fatalf("ledger events = %+v", lr.Events)
	}

	// Revert restores the exact pre-write bytes.
	rr = postCTA(t, s, "/api/briefing/cta/revert", `{"section":"routing","key":"policy"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("revert → %d body=%s", rr.Code, rr.Body.String())
	}
	cfg, err = config.Load(config.LoadOptions{GlobalPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Routing.Policy != "value" {
		t.Fatalf("policy after revert = %q, want value", cfg.Routing.Policy)
	}
}

func TestBriefingCTARevertAllRestores(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.WriteToml(cfgPath, config.Default()); err != nil {
		t.Fatal(err)
	}
	s.opts.ConfigPath = cfgPath
	seedRoutingShadow(t, st, 2, "frugal", 0.5)

	if rr := postCTA(t, s, "/api/briefing/cta/apply", `{"dedup_key":"routing_shadow_1","change_id":"policy"}`); rr.Code != http.StatusOK {
		t.Fatalf("apply → %d body=%s", rr.Code, rr.Body.String())
	}
	rr := postCTA(t, s, "/api/briefing/cta/revert", `{"all":true}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("revert all → %d body=%s", rr.Code, rr.Body.String())
	}
	var out struct {
		Restored int `json:"restored"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Restored != 1 {
		t.Fatalf("restored = %d, want 1", out.Restored)
	}
	cfg, err := config.Load(config.LoadOptions{GlobalPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Routing.Policy != "value" {
		t.Fatalf("policy after revert all = %q, want value", cfg.Routing.Policy)
	}
}

func TestBriefingCTARejections(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.WriteToml(cfgPath, config.Default()); err != nil {
		t.Fatal(err)
	}
	s.opts.ConfigPath = cfgPath

	// Already-active apply needs a config where the target lever is live:
	// mode already enforced + shadow policy fast → mode lever is a no-op.
	cfg := config.Default()
	cfg.Routing.Mode = "enforce"
	enforced := filepath.Join(t.TempDir(), "enforced.toml")
	if err := config.WriteToml(enforced, cfg); err != nil {
		t.Fatal(err)
	}
	s2, st2 := newBriefingServer(t, e)
	s2.opts.ConfigPath = enforced
	seedRoutingShadow(t, st2, 2, "fast", 0.5)

	seedRoutingShadow(t, st, 2, "fast", 0.5) // appliable plan
	seedBriefingTurn(t, st, "s1", "claude", "claude-opus-4",
		time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC), 20_000, 8_000, 0, 0)

	cases := []struct {
		name, method, path, body string
		srv                      *Server
		want                     int
	}{
		{"apply already active", http.MethodPost, "/api/briefing/cta/apply", `{"dedup_key":"routing_shadow_1","change_id":"mode"}`, s2, http.StatusConflict},
		{"apply missing change_id", http.MethodPost, "/api/briefing/cta/apply", `{"dedup_key":"routing_shadow_1"}`, s, http.StatusBadRequest},
		{"apply unknown change_id", http.MethodPost, "/api/briefing/cta/apply", `{"dedup_key":"routing_shadow_1","change_id":"nope"}`, s, http.StatusNotFound},
		{"apply no lever", http.MethodPost, "/api/briefing/cta/apply", `{"dedup_key":"trivial_model_s1"}`, s, http.StatusUnprocessableEntity},
		{"revert missing", http.MethodPost, "/api/briefing/cta/revert", `{"section":"guard","key":"mode"}`, s, http.StatusNotFound},
		{"revert no section", http.MethodPost, "/api/briefing/cta/revert", `{"key":"policy"}`, s, http.StatusBadRequest},
		{"revert wrong method", http.MethodGet, "/api/briefing/cta/revert", ``, s, http.StatusMethodNotAllowed},
		{"apply wrong method", http.MethodGet, "/api/briefing/cta/apply", ``, s, http.StatusMethodNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			tc.srv.Handler().ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("%s %s → %d body=%s, want %d", tc.method, tc.path, rr.Code, rr.Body.String(), tc.want)
			}
		})
	}
}

// TestBriefingCTAApplyNoConfigPath pins the "no file to save to"
// refusal when the server has no ConfigPath configured.
func TestBriefingCTAApplyNoConfigPath(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	seedRoutingShadow(t, st, 2, "fast", 0.5) // appliable, but no config file
	rr := postCTA(t, s, "/api/briefing/cta/apply", `{"dedup_key":"routing_shadow_1","change_id":"mode"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("apply without config path → %d, want 409 (%s)", rr.Code, rr.Body.String())
	}
}

// TestBriefingCTAApplyStickinessWritesReverts applies the stickiness
// lever (min_turns 5→4) into the real config, then reverts it back.
func TestBriefingCTAApplyStickinessWritesReverts(t *testing.T) {
	e := newBriefingTestEnv(t)
	s, st := newBriefingServer(t, e)
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.WriteToml(cfgPath, config.Default()); err != nil {
		t.Fatal(err)
	}
	s.opts.ConfigPath = cfgPath
	seedRoutingShadow(t, st, 1, "fast", 0.5)
	seedStickinessHolds(t, st, 2)

	rr := postCTA(t, s, "/api/briefing/cta/apply", `{"dedup_key":"routing_shadow_1","change_id":"stickiness"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("apply → %d body=%s", rr.Code, rr.Body.String())
	}
	cfg, err := config.Load(config.LoadOptions{GlobalPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Routing.Stickiness.MinTurnsBetweenSwitches != 4 {
		t.Fatalf("min_turns after apply = %d, want 4", cfg.Routing.Stickiness.MinTurnsBetweenSwitches)
	}

	rr = postCTA(t, s, "/api/briefing/cta/revert", `{"section":"routing","key":"stickiness.min_turns_between_switches"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("revert → %d body=%s", rr.Code, rr.Body.String())
	}
	cfg, err = config.Load(config.LoadOptions{GlobalPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Routing.Stickiness.MinTurnsBetweenSwitches != 5 {
		t.Fatalf("min_turns after revert = %d, want 5", cfg.Routing.Stickiness.MinTurnsBetweenSwitches)
	}
}
