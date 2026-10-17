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

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
)

// TestHandleConfigSection_Routing pins the R1.1 routing section
// (security-routing usability arc). The hard invariants:
//
//  1. The complex / secret-bearing [routing] sub-shapes — key_pool
//     (API keys), tiers, local_upstreams, [[routing.rules]], budget
//     scopes, privacy rules — survive a PUT whose body zeroes or
//     omits them: they are config-file-only and the dashboard must
//     never be able to rewrite them.
//  2. Closed vocabularies (mode, policy template) and the shape
//     bounds config.Load enforces (headroom_pct, min_samples) reject
//     with 400 at the PUT, not at the next daemon start.
//  3. The knobs the form does own (gate/mode/policy/retention/
//     stickiness/calibration/rate-limit window) land in the file.
func TestHandleConfigSection_Routing(t *testing.T) {
	tdir := t.TempDir()
	cfgPath := filepath.Join(tdir, "config.toml")
	seed := `[observer]
log_level = "info"

[routing]
enabled = true
mode = "advise"
policy = "value"

[routing.tiers]
"claude-opus-4-8" = "flagship"

[routing.key_pool]
anthropic = ["sk-ant-prior-1", "sk-ant-prior-2"]

[[routing.local_upstreams]]
name = "ollama"
base_url = "http://localhost:11434/v1"
models = ["qwen3:32b"]

[[routing.rules]]
name = "pin-plan"
when.turn_kind = "plan"
action.pin_tier = "flagship"

[[routing.budget.scopes]]
scope = "global"
limit_usd = 100.0
window = "month"

[[routing.privacy.rules]]
project = "secret-proj"
local_only = true
`
	if err := os.WriteFile(cfgPath, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	database, err := openTestDB(context.Background(), db.Options{Path: filepath.Join(tdir, "d.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	server, err := New(Options{DB: database, ConfigPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}

	// Body deliberately zeroes every file-only shape (a real UI draft
	// round-trips them from GET /api/config; a hostile/buggy one might
	// send zeros — either way they must not land).
	// A build without proxy.routing knows no policy templates, only "custom"
	// (routingpolicy_omit.go), and refuses a template name at the PUT (D-535).
	policy := "frugal"
	if len(routingPolicyTemplateNames()) == 0 {
		policy = "custom"
		rr := httptest.NewRecorder()
		server.Handler().ServeHTTP(rr, confirmedReq(t, server, httptest.NewRequest(http.MethodPut,
			"/api/config/section/routing", strings.NewReader(`{"Enabled":true,"Mode":"advise","Policy":"frugal"}`))))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("a template policy in a build without proxy.routing must 400, got %d %s", rr.Code, rr.Body.String())
		}
	}
	body := `{"Enabled":true,"Mode":"enforce","Policy":"` + policy + `","DecisionLogRetentionDays":365,` +
		`"Tiers":{},"BenchmarkFiles":[],"KeyPool":{},"LocalUpstreams":[],"Rules":[],` +
		`"Budget":{"Scopes":[]},"Privacy":{"Rules":[]},"PathClasses":{},` +
		`"Stickiness":{"RespectCache":false,"MinTurnsBetweenSwitches":9},` +
		`"RateLimitWindow":{"Enabled":true,"HeadroomPct":20},` +
		`"Calibration":{"Enabled":true,"MinSamples":75,"AutoDemote":false}}`
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr,
		confirmedReq(t, server, httptest.NewRequest(http.MethodPut, "/api/config/section/routing", strings.NewReader(body))))
	if rr.Code != 200 {
		t.Fatalf("PUT routing: %d body=%s", rr.Code, rr.Body.String())
	}
	var saved struct {
		RestartRequired bool `json:"restart_required"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&saved); err != nil {
		t.Fatal(err)
	}
	if !saved.RestartRequired {
		t.Errorf("routing saves must report restart_required=true (no hot-reload seam)")
	}

	reloaded, err := config.Load(config.LoadOptions{GlobalPath: cfgPath})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	rc := reloaded.Routing

	// 1. File-only shapes survived the zeroing body.
	if len(rc.KeyPool["anthropic"]) != 2 {
		t.Errorf("key_pool must survive a routing section save: %+v", rc.KeyPool)
	}
	if rc.Tiers["claude-opus-4-8"] != "flagship" {
		t.Errorf("tiers must survive: %+v", rc.Tiers)
	}
	if len(rc.LocalUpstreams) != 1 || rc.LocalUpstreams[0].Name != "ollama" {
		t.Errorf("local_upstreams must survive: %+v", rc.LocalUpstreams)
	}
	if len(rc.Rules) != 1 || rc.Rules[0].Name != "pin-plan" {
		t.Errorf("custom rules must survive: %+v", rc.Rules)
	}
	if len(rc.Budget.Scopes) != 1 || rc.Budget.Scopes[0].LimitUSD != 100 {
		t.Errorf("budget scopes must survive: %+v", rc.Budget.Scopes)
	}
	if len(rc.Privacy.Rules) != 1 || rc.Privacy.Rules[0].Project != "secret-proj" {
		t.Errorf("privacy rules must survive: %+v", rc.Privacy.Rules)
	}

	// 3. The knobs the section owns did land.
	if !rc.Enabled || rc.Mode != "enforce" || rc.Policy != policy {
		t.Errorf("gate/mode/policy not persisted: enabled=%v mode=%q policy=%q", rc.Enabled, rc.Mode, rc.Policy)
	}
	if rc.DecisionLogRetentionDays != 365 {
		t.Errorf("retention not persisted: %d", rc.DecisionLogRetentionDays)
	}
	if rc.Stickiness.RespectCache || rc.Stickiness.MinTurnsBetweenSwitches != 9 {
		t.Errorf("stickiness not persisted: %+v", rc.Stickiness)
	}
	if !rc.RateLimitWindow.Enabled || rc.RateLimitWindow.HeadroomPct != 20 {
		t.Errorf("rate_limit_window not persisted: %+v", rc.RateLimitWindow)
	}
	if rc.Calibration.MinSamples != 75 || rc.Calibration.AutoDemote {
		t.Errorf("calibration not persisted: %+v", rc.Calibration)
	}

	// 2. Closed vocabularies + shape bounds reject at the PUT.
	for _, bad := range []string{
		`{"Mode":"yolo","Policy":"value"}`,
		`{"Mode":"advise","Policy":"supersmart"}`,
		`{"Mode":"advise","Policy":""}`,
		`{"Mode":"advise","Policy":"value","RateLimitWindow":{"HeadroomPct":150}}`,
		`{"Mode":"advise","Policy":"value","Calibration":{"MinSamples":-1}}`,
	} {
		rr = httptest.NewRecorder()
		server.Handler().ServeHTTP(rr,
			httptest.NewRequest(http.MethodPut, "/api/config/section/routing", strings.NewReader(bad)))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("invalid body must 400, got %d (%s)", rr.Code, bad)
		}
	}

	// editable_sections advertises routing.
	rr = httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var got struct {
		EditableSections []string `json:"editable_sections"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range got.EditableSections {
		if s == "routing" {
			found = true
		}
	}
	if !found {
		t.Errorf("editable_sections must advertise routing: %v", got.EditableSections)
	}
}
