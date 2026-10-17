package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/clientwire"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/hook"
	"github.com/marmutapp/superbased-observer/internal/integration"
)

// wiringTestServer is a dashboard whose hook registry and wizard writes are
// sandboxed into a temp home. skip is the daemon's owes-live filter.
func wiringTestServer(t *testing.T, catalog []string, skip func(tool, event string) bool) (*Server, string) {
	t.Helper()
	tdir := t.TempDir()
	home := filepath.Join(tdir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	prev := setupWizardHome
	setupWizardHome = home
	t.Cleanup(func() { setupWizardHome = prev })
	cfgPath := filepath.Join(tdir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[observer]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	database, err := openTestDB(context.Background(), db.Options{Path: filepath.Join(tdir, "d.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	entries := make([]ToolCatalogEntry, 0, len(catalog))
	for _, tool := range catalog {
		entries = append(entries, ToolCatalogEntry{Tool: tool})
	}
	regOpts := hook.Options{BinaryPath: "/opt/observer/bin/observer", HomeDir: home, ConfigPath: cfgPath, SkipEvent: skip}
	server, err := New(Options{
		DB:                  database,
		ConfigPath:          cfgPath,
		ToolCatalog:         entries,
		HookRegistryOptions: func() (hook.Options, error) { return regOpts, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return server, home
}

// TestConnectedToolsWiringFollowsRegistry pins T-421 item 2: the Set up
// actions come from the registry's hook mechanisms and MCP formats (the
// predicates `observer init` / `observer uninstall` use), not from a
// claude-code / cursor / codex list. droid gets hooks and MCP, qwen-code its
// hooks, opencode its MCP entry and an MCP cell, codex the route step, aider
// nothing, and a tool whose hooks serve only the prompt gate (off here) is
// not offered the write but says why, while the removal still is.
func TestConnectedToolsWiringFollowsRegistry(t *testing.T) {
	promptOnly := promptOnlyHookTool(t)
	catalog := []string{"droid", "qwen-code", "opencode", "codex", "cursor", "aider"}
	if promptOnly != "" {
		catalog = append(catalog, promptOnly)
	}
	got := toolsStatusWiring(t, catalog, nil)
	if droid := got["droid"].Wiring; droid == nil || !droid.Hooks.Register || !droid.Hooks.Remove || !droid.MCP.Register {
		t.Errorf("droid wiring = %+v, want hooks and MCP", droid)
	}
	if po := got[promptOnly].Wiring; promptOnly != "" && (po == nil || po.Hooks.Register || po.Hooks.Note == "" || !po.Hooks.Remove) {
		t.Errorf("%s (prompt-gate-only hooks, prompt lane off) wiring = %+v, want a removable hook, no hook write and a note", promptOnly, po)
	}
	if qw := got["qwen-code"].Wiring; qw == nil || !qw.Hooks.Register || !qw.Hooks.Remove {
		t.Errorf("qwen-code wiring = %+v, want its hooks step", qw)
	}
	if oc := got["opencode"]; oc.Wiring == nil || !oc.Wiring.MCP.Register || oc.MCP == nil {
		t.Errorf("opencode wiring = %+v mcp probe = %+v, want an MCP step and an MCP cell", oc.Wiring, oc.MCP)
	}
	if cx := got["codex"].Wiring; cx == nil || cx.RouteEndpoint != "/api/setup/codex" || !cx.Route.Register || !cx.Route.Remove {
		t.Errorf("codex wiring = %+v, want the route step", cx)
	}
	if cu := got["cursor"].Wiring; cu == nil || cu.Route.Register || cu.RouteEndpoint != "" {
		t.Errorf("cursor wiring = %+v, want no route step (no persisted route)", cu)
	}
	if ai := got["aider"].Wiring; ai != nil {
		t.Errorf("aider has no hook or MCP surface; wiring = %+v", ai)
	}
}

// TestConnectedToolsWiringKeepsOwesLiveGatesUnwritten: a hook whose bindings
// all still owe a live test (the daemon's owes-live filter) is not offered
// for registration, exactly as `observer start` leaves it unwritten.
func TestConnectedToolsWiringKeepsOwesLiveGatesUnwritten(t *testing.T) {
	owesLive := func(tool, _ string) bool { return tool == "qwen-code" }
	got := toolsStatusWiring(t, []string{"qwen-code"}, owesLive)
	if qw := got["qwen-code"].Wiring; qw == nil || qw.Hooks.Register || qw.Hooks.Note == "" {
		t.Errorf("qwen-code wiring under an owes-live filter = %+v, want no hook write and a note", qw)
	}
}

type wiringRow = struct {
	Tool string `json:"tool"`
	MCP  *struct {
		Registered bool   `json:"registered"`
		Detail     string `json:"detail"`
	} `json:"mcp"`
	Wiring *struct {
		Hooks struct {
			Register bool   `json:"register"`
			Remove   bool   `json:"remove"`
			Note     string `json:"note"`
		} `json:"hooks"`
		MCP struct {
			Register bool `json:"register"`
			Remove   bool `json:"remove"`
		} `json:"mcp"`
		Route struct {
			Register bool `json:"register"`
			Remove   bool `json:"remove"`
		} `json:"route"`
		RouteEndpoint string `json:"route_endpoint"`
	} `json:"wiring"`
}

// promptOnlyHookTool picks, by capability shape, a tool the wizard offers for
// hooks whose wired bindings are all prompt-gate bindings ("" when the
// registry has none, so the assertions that need one are skipped, not the
// test).
func promptOnlyHookTool(t *testing.T) string {
	t.Helper()
	for _, c := range integration.Capabilities() {
		if clientwire.HookSupported(c.Tool) && c.Hook.WiredPromptOnly() {
			return c.Tool
		}
	}
	return "" // none today: every prompt-gate mechanism also wires a lifecycle binding
}

func toolsStatusWiring(t *testing.T, catalog []string, skip func(tool, event string) bool) map[string]wiringRow {
	t.Helper()
	server, _ := wiringTestServer(t, catalog, skip)
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/tools/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Tools []wiringRow `json:"tools"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	out := map[string]wiringRow{}
	for _, r := range got.Tools {
		out[r.Tool] = r
	}
	return out
}

// assertUnwantedRegistration: a registration with nothing wanted answers 200
// skipped with the reason and writes nothing under home.
func assertUnwantedRegistration(t *testing.T, server *Server, home, tool string) {
	t.Helper()
	rr := wizardPost(t, server, "/api/setup/hooks", `{"tool":"`+tool+`"}`)
	var out struct {
		Skipped    bool   `json:"skipped"`
		SkipReason string `json:"skip_reason"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil || rr.Code != http.StatusOK || !out.Skipped || out.SkipReason == "" {
		t.Errorf("%s (unwanted) = %d %+v, want 200 skipped with a reason", tool, rr.Code, out)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Errorf("an unwanted registration still wrote under the home: %v", entries)
	}
}

// TestSetupWizardSkipsOwesLiveRegistration: a tool whose every binding owes a
// live test (the daemon's filter) is answered with the reason, unwritten.
func TestSetupWizardSkipsOwesLiveRegistration(t *testing.T) {
	server, home := wiringTestServer(t, nil, func(tool, _ string) bool { return tool == "qwen-code" })
	assertUnwantedRegistration(t, server, home, "qwen-code")
}

// TestSetupWizardRegistersEveryAdapter: the wizard's write endpoints accept
// every tool the registry wires (qwen-code hooks, opencode MCP), refuse a
// tool with no surface, and answer an unwanted registration (prompt-gate-only
// hooks with the prompt lane off) with the reason and no write.
func TestSetupWizardRegistersEveryAdapter(t *testing.T) {
	server, home := wiringTestServer(t, nil, nil)
	if rr := wizardPost(t, server, "/api/setup/hooks", `{"tool":"qwen-code","dry_run":true}`); rr.Code != http.StatusOK {
		t.Errorf("qwen-code hooks dry run: %d %s", rr.Code, rr.Body.String())
	}
	if rr := wizardPost(t, server, "/api/setup/mcp", `{"tool":"opencode","dry_run":true}`); rr.Code != http.StatusOK {
		t.Errorf("opencode MCP dry run: %d %s", rr.Code, rr.Body.String())
	}
	if rr := wizardPost(t, server, "/api/setup/hooks", `{"tool":"aider"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("aider hooks: %d, want 400", rr.Code)
	}
	if promptOnly := promptOnlyHookTool(t); promptOnly != "" {
		assertUnwantedRegistration(t, server, home, promptOnly)
	}
	// A real qwen-code write goes through the daemon's own registry options.
	if rr := wizardPost(t, server, "/api/setup/hooks", `{"tool":"qwen-code"}`); rr.Code != http.StatusOK {
		t.Fatalf("qwen-code hooks: %d %s", rr.Code, rr.Body.String())
	}
	if b, err := os.ReadFile(filepath.Join(home, ".qwen", "settings.json")); err != nil || len(b) == 0 {
		t.Errorf("qwen-code hooks not written: %v", err)
	}
}
