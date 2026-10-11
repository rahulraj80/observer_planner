package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
)

// newProfileAssignTestServer is newProfilesTestServer with the config-saved
// hook wired, so a test can see the hot-reload poke.
func newProfileAssignTestServer(t *testing.T) (*Server, string, *atomic.Int32) {
	t.Helper()
	tdir := t.TempDir()
	cfgPath := filepath.Join(tdir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[observer]\nlog_level = \"warn\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	database, err := openTestDB(context.Background(), db.Options{Path: filepath.Join(tdir, "d.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	var pokes atomic.Int32
	server, err := New(Options{DB: database, ConfigPath: cfgPath, OnConfigSaved: func() { pokes.Add(1) }})
	if err != nil {
		t.Fatal(err)
	}
	return server, cfgPath, &pokes
}

func decodeAssignments(t *testing.T, body []byte) profileAssignmentsResponse {
	t.Helper()
	var out profileAssignmentsResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return out
}

// TestProfileAssignmentsCycle pins T-369: the dashboard assigns a profile
// to one tool, the assignment lands in [profiles.by_tool] of config.toml
// and resolves for that tool, the daemon's reload hook fires, and removing
// it falls the tool back to its provider assignment.
func TestProfileAssignmentsCycle(t *testing.T) {
	server, cfgPath, pokes := newProfileAssignTestServer(t)

	rr := profilesReq(t, server, http.MethodGet, "/api/config/profile-assignments", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", rr.Code, rr.Body.String())
	}
	got := decodeAssignments(t, rr.Body.Bytes())
	if len(got.ByTool) != 0 || got.ByProvider["anthropic"] != "claude-code" || got.Default != "default" {
		t.Fatalf("initial table = %+v", got)
	}
	if !slices.Contains(got.ProfileNames, "codex-variant") {
		t.Fatalf("profile_names = %v, want the built-ins", got.ProfileNames)
	}
	if !slices.ContainsFunc(got.Tools, func(tl profileAssignTool) bool { return tl.ID == "cline" }) {
		t.Fatalf("tools = %v, want the registry vocabulary incl. cline", got.Tools)
	}

	rr = profilesReq(t, server, http.MethodPut, "/api/config/profile-assignments",
		`{"target":"tool:cline","profile":"codex-variant"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rr.Code, rr.Body.String())
	}
	got = decodeAssignments(t, rr.Body.Bytes())
	if got.ByTool["cline"] != "codex-variant" || !got.Changed {
		t.Fatalf("PUT response = %+v", got)
	}
	if pokes.Load() != 1 {
		t.Fatalf("config-saved hook fired %d times, want 1", pokes.Load())
	}
	cfg, err := config.Load(config.LoadOptions{GlobalPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if name := config.ResolveProfileName(cfg.Profiles, "anthropic", "cline"); name != "codex-variant" {
		t.Fatalf("persisted resolution for cline = %q, want codex-variant", name)
	}
	if cfg.Profiles.ByProvider["anthropic"] != "claude-code" {
		t.Fatalf("provider assignment disturbed: %+v", cfg.Profiles)
	}
	raw, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(raw), "[profiles.by_tool]") {
		t.Fatalf("config.toml lacks [profiles.by_tool]:\n%s", raw)
	}

	// Re-assigning the same profile is a no-op: no rewrite, no poke.
	rr = profilesReq(t, server, http.MethodPut, "/api/config/profile-assignments",
		`{"target":"tool:cline","profile":"codex-variant"}`)
	if rr.Code != http.StatusOK || decodeAssignments(t, rr.Body.Bytes()).Changed || pokes.Load() != 1 {
		t.Fatalf("same-value PUT: %d %s pokes=%d, want an unchanged no-op", rr.Code, rr.Body.String(), pokes.Load())
	}

	rr = profilesReq(t, server, http.MethodDelete, "/api/config/profile-assignments", `{"target":"tool:cline"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("DELETE: %d %s", rr.Code, rr.Body.String())
	}
	if got = decodeAssignments(t, rr.Body.Bytes()); len(got.ByTool) != 0 || !got.Changed {
		t.Fatalf("DELETE response = %+v", got)
	}
	cfg, err = config.Load(config.LoadOptions{GlobalPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if name := config.ResolveProfileName(cfg.Profiles, "anthropic", "cline"); name != "claude-code" {
		t.Fatalf("after removal cline resolves to %q, want the provider's claude-code", name)
	}
	if pokes.Load() != 2 {
		t.Fatalf("config-saved hook fired %d times, want 2", pokes.Load())
	}
}

// TestProfileAssignmentsRefusals: the sanity checks before any write.
func TestProfileAssignmentsRefusals(t *testing.T) {
	server, cfgPath, pokes := newProfileAssignTestServer(t)
	before, _ := os.ReadFile(cfgPath)
	cases := []struct {
		name, method, body string
		want               int
	}{
		{"unknown profile", http.MethodPut, `{"target":"tool:cline","profile":"no-such"}`, http.StatusBadRequest},
		{"unknown tool", http.MethodPut, `{"target":"tool:not-a-tool","profile":"codex-safe"}`, http.StatusBadRequest},
		{"bad target", http.MethodPut, `{"target":"gemini","profile":"codex-safe"}`, http.StatusBadRequest},
		{"empty tool", http.MethodPut, `{"target":"tool:","profile":"codex-safe"}`, http.StatusBadRequest},
		{"remove a provider tier", http.MethodDelete, `{"target":"anthropic"}`, http.StatusBadRequest},
		{"bad json", http.MethodPut, `{`, http.StatusBadRequest},
		{"method guard", http.MethodPost, `{}`, http.StatusMethodNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := profilesReq(t, server, tc.method, "/api/config/profile-assignments", tc.body)
			if rr.Code != tc.want {
				t.Fatalf("%s: %d %s, want %d", tc.name, rr.Code, rr.Body.String(), tc.want)
			}
		})
	}
	after, _ := os.ReadFile(cfgPath)
	if string(before) != string(after) || pokes.Load() != 0 {
		t.Fatalf("a refused request wrote the config or poked the daemon (pokes=%d)", pokes.Load())
	}
}
