package dashboard

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// seedProject inserts a known project root into the server's database.
func seedProject(t *testing.T, s *Server, root string) {
	t.Helper()
	if _, err := s.opts.DB.Exec(`INSERT INTO projects (root_path, created_at) VALUES (?, ?)`,
		root, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
}

func decodeProjectEdit(t *testing.T, body []byte) projectProfileEditResponse {
	t.Helper()
	var out projectProfileEditResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return out
}

// TestProjectProfilesCycle pins T-421 item 1: the dashboard lists a known
// project, previews an assignment (nothing written), applies exactly that
// preview into <root>/.observer/config.toml (key-preserving, so the project's
// own compression keys survive and nothing else is added), the daemon's
// project-layer resolution picks it up, and removal falls back to global.
func TestProjectProfilesCycle(t *testing.T) {
	server, cfgPath, _ := newProfileAssignTestServer(t)
	root := t.TempDir()
	seedProject(t, server, root)
	overlay := filepath.Join(root, config.ProjectOverlayFilename)
	if err := os.MkdirAll(filepath.Dir(overlay), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overlay, []byte("[compression.conversation]\ntarget_ratio = 0.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	globalBefore, _ := os.ReadFile(cfgPath)

	rr := profilesReq(t, server, http.MethodGet, "/api/config/project-profiles", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", rr.Code, rr.Body.String())
	}
	var list projectProfilesResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Projects) != 1 || list.Projects[0].Root != root || !list.Projects[0].HasFile || list.Projects[0].Default != "" || list.Projects[0].Refusal != "" {
		t.Fatalf("projects = %+v", list.Projects)
	}
	if len(list.Providers) == 0 || len(list.ProfileNames) == 0 || list.GlobalDefault == "" {
		t.Fatalf("vocabulary = %+v", list)
	}

	body := `{"root":` + jsonString(root) + `,"target":"default","profile":"codex-variant","preview":true}`
	rr = profilesReq(t, server, http.MethodPut, "/api/config/project-profiles", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rr.Code, rr.Body.String())
	}
	prev := decodeProjectEdit(t, rr.Body.Bytes())
	if !prev.Changed || prev.Applied || !strings.Contains(prev.After, "codex-variant") || prev.BaseSHA == "" || prev.Key != "profiles.default" ||
		!strings.Contains(prev.Diff, `default = "codex-variant"`) || !strings.Contains(prev.Diff, "\n+") {
		t.Fatalf("preview = %+v", prev)
	}
	if raw, _ := os.ReadFile(overlay); strings.Contains(string(raw), "codex-variant") {
		t.Fatal("the preview wrote the file")
	}

	body = `{"root":` + jsonString(root) + `,"target":"default","profile":"codex-variant","base_sha":"` + prev.BaseSHA + `"}`
	rr = profilesReq(t, server, http.MethodPut, "/api/config/project-profiles", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", rr.Code, rr.Body.String())
	}
	applied := decodeProjectEdit(t, rr.Body.Bytes())
	if !applied.Applied || applied.Project == nil || applied.Project.Default != "codex-variant" {
		t.Fatalf("apply = %+v", applied)
	}
	raw, _ := os.ReadFile(overlay)
	if pp := config.ProjectProfiles(config.ProfilesConfig{Default: "default"}, raw); pp.Default != "codex-variant" {
		t.Fatalf("project layer resolves to %q", pp.Default)
	}
	if got, err := config.ProjectCompression(config.Default().Compression, raw); err != nil || got.Conversation.TargetRatio != 0.5 {
		t.Fatalf("the project's own key was lost: %v %v", got.Conversation.TargetRatio, err)
	}
	if strings.Contains(string(raw), "shell") {
		t.Fatalf("the apply added keys the file never had:\n%s", raw)
	}
	if globalAfter, _ := os.ReadFile(cfgPath); string(globalAfter) != string(globalBefore) {
		t.Fatal("a project assignment touched the global config")
	}

	// Remove: preview, then apply.
	body = `{"root":` + jsonString(root) + `,"target":"default","preview":true}`
	rr = profilesReq(t, server, http.MethodDelete, "/api/config/project-profiles", body)
	rem := decodeProjectEdit(t, rr.Body.Bytes())
	if rr.Code != http.StatusOK || !rem.Changed {
		t.Fatalf("remove preview: %d %s", rr.Code, rr.Body.String())
	}
	body = `{"root":` + jsonString(root) + `,"target":"default","base_sha":"` + rem.BaseSHA + `"}`
	rr = profilesReq(t, server, http.MethodDelete, "/api/config/project-profiles", body)
	if rr.Code != http.StatusOK || decodeProjectEdit(t, rr.Body.Bytes()).Project.Default != "" {
		t.Fatalf("remove: %d %s", rr.Code, rr.Body.String())
	}
}

// TestProjectProfilesRefusals: the sanity checks before any write.
func TestProjectProfilesRefusals(t *testing.T) {
	server, _, _ := newProfileAssignTestServer(t)
	root := t.TempDir()
	seedProject(t, server, root)
	stranger := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	seedProject(t, server, home)

	cases := []struct {
		name, method, body string
		want               int
	}{
		{"unknown root", http.MethodPut, `{"root":` + jsonString(stranger) + `,"target":"default","profile":"codex-safe","preview":true}`, http.StatusBadRequest},
		{"relative traversal", http.MethodPut, `{"root":` + jsonString(root+"/../") + `,"target":"default","profile":"codex-safe","preview":true}`, http.StatusBadRequest},
		{"home directory", http.MethodPut, `{"root":` + jsonString(home) + `,"target":"default","profile":"codex-safe","preview":true}`, http.StatusBadRequest},
		{"unknown profile", http.MethodPut, `{"root":` + jsonString(root) + `,"target":"default","profile":"no-such","preview":true}`, http.StatusBadRequest},
		{"unknown tool", http.MethodPut, `{"root":` + jsonString(root) + `,"target":"tool:not-a-tool","profile":"codex-safe","preview":true}`, http.StatusBadRequest},
		{"bad target", http.MethodPut, `{"root":` + jsonString(root) + `,"target":"gemini","profile":"codex-safe","preview":true}`, http.StatusBadRequest},
		{"apply without preview", http.MethodPut, `{"root":` + jsonString(root) + `,"target":"default","profile":"codex-safe"}`, http.StatusBadRequest},
		{"stale preview", http.MethodPut, `{"root":` + jsonString(root) + `,"target":"default","profile":"codex-safe","base_sha":"00"}`, http.StatusConflict},
		{"bad json", http.MethodPut, `{`, http.StatusBadRequest},
		{"method guard", http.MethodPost, `{}`, http.StatusMethodNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := profilesReq(t, server, tc.method, "/api/config/project-profiles", tc.body)
			if rr.Code != tc.want {
				t.Fatalf("%d %s, want %d", rr.Code, rr.Body.String(), tc.want)
			}
		})
	}
	for _, dir := range []string{root, stranger, home} {
		if _, err := os.Stat(filepath.Join(dir, config.ProjectOverlayFilename)); err == nil {
			t.Fatalf("a refused request wrote %s", dir)
		}
	}

	// The list states why the home directory cannot be written from here.
	rr := profilesReq(t, server, http.MethodGet, "/api/config/project-profiles", "")
	var list projectProfilesResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	for _, p := range list.Projects {
		if p.Root == home && !strings.Contains(p.Refusal, "home directory") {
			t.Fatalf("home row refusal = %q", p.Refusal)
		}
		if p.Root == root && p.Refusal != "" {
			t.Fatalf("writable root shows a refusal: %q", p.Refusal)
		}
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
