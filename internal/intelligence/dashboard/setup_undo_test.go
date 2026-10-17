package dashboard

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetupRouteUndo is the dashboard half of the proxy-route undo (backlog
// item 6, sweep row 18): POST {"undo": true} to /api/setup/claude or
// /api/setup/codex removes the route through the same proxyroute seam
// `observer uninstall` uses. A dry run previews without writing; a real
// removal leaves a manage audit row.
func TestSetupRouteUndo(t *testing.T) {
	cases := []struct {
		name, path, file string
		seed             func(home string)
	}{
		{
			name: "claude", path: "/api/setup/claude", file: filepath.Join(".claude", "settings.json"),
			seed: func(home string) {
				writeUndoFixture(t, filepath.Join(home, ".claude", "settings.json"),
					`{"model":"opus","env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:8820"}}`)
			},
		},
		{
			name: "codex", path: "/api/setup/codex", file: filepath.Join(".codex", "config.toml"),
			seed: func(home string) {
				writeUndoFixture(t, filepath.Join(home, ".codex", "config.toml"),
					"model = \"gpt-5\"\nmodel_provider = \"openai-observer\"\n\n[model_providers.openai-observer]\nbase_url = \"http://127.0.0.1:8820/v1\"\n")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := withFakeClaudeHome(t, "")
			t.Setenv("CODEX_HOME", "")
			tc.seed(home)
			srv := newSetupServer(t, 8820)
			target := filepath.Join(home, tc.file)
			before, _ := os.ReadFile(target)

			post := func(body string) *httptest.ResponseRecorder {
				rr := httptest.NewRecorder()
				srv.Handler().ServeHTTP(rr, armLocalWrite(httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(body))))
				return rr
			}
			// Dry run: says it would remove, writes nothing.
			rr := post(`{"undo":true,"dry_run":true}`)
			if rr.Code != http.StatusOK {
				t.Fatalf("dry run = %d %s", rr.Code, rr.Body.String())
			}
			var resp struct {
				Removed bool `json:"removed"`
				DryRun  bool `json:"dry_run"`
			}
			decodeJSON(t, rr, &resp)
			if !resp.Removed || !resp.DryRun {
				t.Fatalf("dry run response %+v", resp)
			}
			if after, _ := os.ReadFile(target); string(after) != string(before) {
				t.Fatalf("dry run changed the file:\n%s", after)
			}

			rr = post(`{"undo":true}`)
			if rr.Code != http.StatusOK {
				t.Fatalf("undo = %d %s", rr.Code, rr.Body.String())
			}
			after, _ := os.ReadFile(target)
			if strings.Contains(string(after), "8820") {
				t.Errorf("route still present after undo:\n%s", after)
			}
			if !strings.Contains(string(after), "model") {
				t.Errorf("unrelated keys were not preserved:\n%s", after)
			}
			var n int
			if err := srv.opts.DB.QueryRow(`SELECT COUNT(*) FROM remote_audit WHERE detail LIKE 'proxy_route_undo%'`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Errorf("audit rows = %d, want 1 (the dry run is not audited)", n)
			}

			// Nothing left to remove: a second undo is a no-op, not an error.
			rr = post(`{"undo":true}`)
			decodeJSON(t, rr, &resp)
			if rr.Code != http.StatusOK || resp.Removed {
				t.Errorf("second undo = %d %+v, want 200 and removed=false", rr.Code, resp)
			}
		})
	}
}

func writeUndoFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
