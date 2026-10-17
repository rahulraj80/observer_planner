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

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/hook"
)

type hooksCellWire struct {
	Tools []struct {
		Tool  string `json:"tool"`
		Hooks *struct {
			Registered bool   `json:"registered"`
			Partial    bool   `json:"partial"`
			NoHooks    bool   `json:"no_hooks"`
			Unprobed   bool   `json:"unprobed"`
			Detail     string `json:"detail"`
		} `json:"hooks"`
	} `json:"tools"`
}

// TestToolsStatusHooksFollowRegistry pins T-400: Settings > Connected tools
// used to probe hook state only for claude-code, cursor and codex (a
// tool-name switch), so droid, command-code, qwen-code, goose, cline-cli and
// every other adapter with a registered hook binding showed n/a. The hooks
// cell is now decided by the tool's integration registry row: a tool whose
// mechanism the hook registry writes is dry-run probed (and reads "on" once
// registered), a hookless one says "no hooks" with the registry's reason, and
// a mechanism written outside the registry (Hermes' plugin) says so.
func TestToolsStatusHooksFollowRegistry(t *testing.T) {
	tdir := t.TempDir()
	home := filepath.Join(tdir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(tdir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[observer]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	database, err := openTestDB(context.Background(), db.Options{Path: filepath.Join(tdir, "d.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	regOpts := hook.Options{BinaryPath: "/opt/observer/bin/observer", HomeDir: home, ConfigPath: cfgPath}
	// droid is registered for real first, so its cell must read "on".
	reg, err := hook.NewRegistry(regOpts)
	if err != nil {
		t.Fatal(err)
	}
	if res := reg.Register("droid"); res.Error != nil || len(res.HooksAdded) == 0 {
		t.Fatalf("register droid: err=%v added=%v", res.Error, res.HooksAdded)
	}

	probed := []string{"droid", "command-code", "qwen-code", "goose", "cline-cli"}
	catalog := []ToolCatalogEntry{}
	for _, tool := range append(append([]string{}, probed...), "aider", "hermes") {
		catalog = append(catalog, ToolCatalogEntry{Tool: tool})
	}
	server, err := New(Options{
		DB:                  database,
		ConfigPath:          cfgPath,
		ToolCatalog:         catalog,
		HookRegistryOptions: func() (hook.Options, error) { return regOpts, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/tools/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var got hooksCellWire
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	cells := map[string]int{}
	for i, r := range got.Tools {
		cells[r.Tool] = i
	}

	for _, tool := range probed {
		i, ok := cells[tool]
		if !ok {
			t.Fatalf("no row for %s", tool)
		}
		h := got.Tools[i].Hooks
		if h == nil {
			t.Errorf("%s: hooks cell is n/a; a tool with registered hook bindings must be probed", tool)
			continue
		}
		if h.NoHooks || h.Unprobed || h.Detail == "" || strings.Contains(h.Detail, "not supported") {
			t.Errorf("%s: hooks cell = %+v, want a dry-run probe result", tool, *h)
		}
	}
	if h := got.Tools[cells["droid"]].Hooks; h == nil || !h.Registered {
		t.Errorf("droid (registered above): hooks cell = %+v, want registered", h)
	}
	if h := got.Tools[cells["aider"]].Hooks; h == nil || !h.NoHooks || !strings.Contains(h.Detail, "no hook surface") {
		t.Errorf("aider: hooks cell = %+v, want no_hooks with the registry's reason", h)
	}
	if h := got.Tools[cells["hermes"]].Hooks; h == nil || !h.Unprobed || h.Detail == "" {
		t.Errorf("hermes: hooks cell = %+v, want unprobed (its plugin is written outside the hook registry)", h)
	}
}
