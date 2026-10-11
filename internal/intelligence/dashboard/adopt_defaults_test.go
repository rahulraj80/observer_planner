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
)

func newAdoptServer(t *testing.T, cfgBody string) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, []byte(cfgBody), 0o600); err != nil {
		t.Fatal(err)
	}
	database, err := openTestDB(context.Background(), db.Options{Path: filepath.Join(dir, "d.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	srv, err := New(Options{
		DB: database, ConfigPath: cfg,
		ToolCatalog: []ToolCatalogEntry{{Tool: "claude-code"}, {Tool: "codex"}, {Tool: "zed"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv, cfg
}

type adoptResp struct {
	HadExplicitList bool     `json:"had_explicit_list"`
	Missing         []string `json:"missing"`
	Existing        []string `json:"existing"`
	Skipped         bool     `json:"skipped"`
	Written         bool     `json:"written"`
	RestartRequired bool     `json:"restart_required"`
	Error           string   `json:"error"`
}

// TestAdoptDefaults is the dashboard twin of `observer config adopt-defaults`
// (sweep row 19): GET previews through config.AdoptEnabledAdapters, POST
// writes through config.AdoptEnabledAdaptersFile (the CLI's --write path)
// only when the posted list still matches the preview, and is audited.
func TestAdoptDefaults(t *testing.T) {
	body := "[observer.watch]\nenabled_adapters = [\"claude-code\"] # mine\n"
	srv, cfg := newAdoptServer(t, body)
	do := func(method, payload string) (int, adoptResp) {
		rr := httptest.NewRecorder()
		var req *http.Request
		if payload == "" {
			req = httptest.NewRequest(method, "/api/config/adopt-defaults", nil)
		} else {
			req = httptest.NewRequest(method, "/api/config/adopt-defaults", strings.NewReader(payload))
		}
		srv.Handler().ServeHTTP(rr, armLocalWrite(req))
		var out adoptResp
		_ = json.NewDecoder(rr.Body).Decode(&out)
		return rr.Code, out
	}

	code, prev := do(http.MethodGet, "")
	if code != http.StatusOK || !prev.HadExplicitList || strings.Join(prev.Missing, ",") != "codex,zed" {
		t.Fatalf("preview = %d %+v", code, prev)
	}
	if b, _ := os.ReadFile(cfg); string(b) != body {
		t.Fatal("GET wrote the file")
	}

	// A stale preview (the posted list no longer matches) writes nothing.
	if code, out := do(http.MethodPost, `{"adapters":["codex"]}`); code != http.StatusConflict || out.Written {
		t.Fatalf("stale POST = %d %+v, want 409", code, out)
	}
	if b, _ := os.ReadFile(cfg); string(b) != body {
		t.Fatal("stale POST wrote the file")
	}

	code, out := do(http.MethodPost, `{"adapters":["codex","zed"]}`)
	if code != http.StatusOK || !out.Written || !out.RestartRequired {
		t.Fatalf("POST = %d %+v", code, out)
	}
	after, _ := os.ReadFile(cfg)
	for _, want := range []string{`"codex"`, `"zed"`, "# mine"} {
		if !strings.Contains(string(after), want) {
			t.Errorf("written config lacks %s:\n%s", want, after)
		}
	}
	var n int
	if err := srv.opts.DB.QueryRow(`SELECT COUNT(*) FROM remote_audit WHERE detail LIKE 'config_adopt_defaults%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("audit rows = %d, want 1", n)
	}

	// Nothing left: the preview is empty and a POST is a no-op.
	if _, again := do(http.MethodGet, ""); len(again.Missing) != 0 {
		t.Errorf("after write, missing = %v", again.Missing)
	}
}
