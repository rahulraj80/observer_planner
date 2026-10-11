package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// T-639: POST /api/experiments (start) and POST /api/experiments/stop wrote
// config.toml through config.WriteToml directly, outside the save pipeline:
// no preview, no conflict check, no read-back, no audit row. Both now save
// through commitConfigEdit like the four T-572 writers, keep their 409 / 404
// refusals, and the GET routes the web pages read carry the config_etag the
// page sends back as ?base_etag=.

func TestT639ExperimentWritersUseTheSavePipeline(t *testing.T) {
	cases := []struct {
		name, path, body, wantInDiff, wantKey string
		wantAudit                             int
	}{
		{"start", "/api/experiments", `{"name":"t639-exp","class":"openai","control":"codex-safe","candidate":"codex-variant"}`, "t639-exp", `"started"`, 1},
		{"stop", "/api/experiments/stop", `{"name":"t639-exp"}`, "stopped_at", `"stopped":"t639-exp"`, 2},
	}
	s, h := newManageServer(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before, _ := os.ReadFile(s.opts.ConfigPath)

			// 1. A dry run answers a diff and writes nothing.
			rec := sshProfilesReq(t, h, http.MethodPost, c.path+"?dry_run=1", c.body, false)
			if rec.Code != http.StatusOK {
				t.Fatalf("dry run = %d: %s", rec.Code, rec.Body)
			}
			var dry struct {
				DryRun     bool   `json:"dry_run"`
				WouldWrite bool   `json:"would_write"`
				Diff       string `json:"diff"`
				ConfigEtag string `json:"config_etag"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &dry); err != nil {
				t.Fatal(err)
			}
			if !dry.DryRun || !dry.WouldWrite || !strings.Contains(dry.Diff, c.wantInDiff) || dry.ConfigEtag == "" {
				t.Fatalf("dry run answer = %+v (body %s)", dry, rec.Body)
			}
			if strings.Contains(rec.Body.String(), `"started"`) || strings.Contains(rec.Body.String(), `"stopped"`) {
				t.Fatalf("a dry run claims the write happened: %s", rec.Body)
			}
			if after, _ := os.ReadFile(s.opts.ConfigPath); string(after) != string(before) {
				t.Fatal("a dry run wrote config.toml")
			}

			// 2. The file changes after the preview: the save at the previewed
			// etag is refused and the outside edit survives.
			outside := string(before) + "\n# edited outside the page\n"
			if err := os.WriteFile(s.opts.ConfigPath, []byte(outside), 0o600); err != nil {
				t.Fatal(err)
			}
			rec = sshProfilesReq(t, h, http.MethodPost, c.path+"?base_etag="+dry.ConfigEtag, c.body, false)
			if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "config_changed") {
				t.Fatalf("stale save = %d: %s (want 409 config_changed)", rec.Code, rec.Body)
			}
			if now, _ := os.ReadFile(s.opts.ConfigPath); string(now) != outside {
				t.Fatal("a stale save overwrote the outside edit")
			}

			// 3. A save at the current etag (what the page now sends) writes,
			// keeps the route's own answer fields and leaves an audit row.
			etag := getConfigEtag(t, h, "/api/experiments")
			rec = sshProfilesReq(t, h, http.MethodPost, c.path+"?base_etag="+etag, c.body, false)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"saved":true`) ||
				!strings.Contains(rec.Body.String(), c.wantKey) {
				t.Fatalf("save = %d: %s", rec.Code, rec.Body)
			}
			written, _ := os.ReadFile(s.opts.ConfigPath)
			if !strings.Contains(string(written), c.wantInDiff) {
				t.Fatalf("saved config.toml lacks %q:\n%s", c.wantInDiff, written)
			}
			var n int
			if err := s.opts.DB.QueryRow(`SELECT COUNT(*) FROM remote_audit WHERE detail LIKE 'experiment%'`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != c.wantAudit {
				t.Fatalf("experiment audit rows = %d, want %d", n, c.wantAudit)
			}
		})
	}

	// The refusals keep the statuses the page already handles, write nothing.
	refusals := []struct {
		name, path, body string
		want             int
	}{
		{"duplicate name", "/api/experiments", `{"name":"t639-exp","class":"anthropic","control":"claude-code","candidate":"codex-safe"}`, http.StatusConflict},
		{"stop a stopped one", "/api/experiments/stop", `{"name":"t639-exp"}`, http.StatusConflict},
		{"stop unknown", "/api/experiments/stop", `{"name":"t639-none"}`, http.StatusNotFound},
	}
	for _, c := range refusals {
		t.Run(c.name, func(t *testing.T) {
			before, _ := os.ReadFile(s.opts.ConfigPath)
			rec := sshProfilesReq(t, h, http.MethodPost, c.path, c.body, false)
			if rec.Code != c.want {
				t.Fatalf("%s = %d: %s, want %d", c.name, rec.Code, rec.Body, c.want)
			}
			if after, _ := os.ReadFile(s.opts.ConfigPath); string(after) != string(before) {
				t.Fatal("a refused edit wrote config.toml")
			}
		})
	}
}

// TestT639EditorReadsCarryConfigEtag pins the GET half the web pages use:
// each editor read carries the etag of the file it rendered.
func TestT639EditorReadsCarryConfigEtag(t *testing.T) {
	_, h := newManageServer(t)
	for _, path := range []string{"/api/experiments", "/api/terminal/sandbox/config", "/api/config/profile-assignments"} {
		if got := getConfigEtag(t, h, path); !strings.HasPrefix(got, "sha256:") {
			t.Errorf("GET %s config_etag = %q, want a sha256 etag", path, got)
		}
	}
}

// getConfigEtag reads config_etag from a GET route's answer.
func getConfigEtag(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "127.0.0.1:8080"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body)
	}
	var body struct {
		ConfigEtag string `json:"config_etag"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.ConfigEtag
}
