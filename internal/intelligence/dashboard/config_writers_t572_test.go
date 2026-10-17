package dashboard

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components/buildfeat"
)

// T-572: four dashboard config writers (PUT /api/config/pricing, POST
// /api/process/enable-capture, PUT /api/config/profile-assignments, PUT
// /api/terminal/sandbox/config) rewrote config.toml directly with no
// preview, no conflict check and (all but one) no audit row. Each now goes
// through the ONE save pipeline: a dry run previews the diff and writes
// nothing, a save at a stale base etag is refused instead of overwriting an
// edit made since the preview, and a real save leaves an audit row.

func TestT572WritersUseTheSavePipeline(t *testing.T) {
	cases := []struct {
		name, method, path, body, wantInDiff, auditAction string
	}{
		{"pricing", http.MethodPut, "/api/config/pricing", `{"models":{"t572-model":{"Input":1.5,"Output":3}}}`, "t572-model", "config_pricing"},
		{"process enable", http.MethodPost, "/api/process/enable-capture", ``, "observer.process", "process_enable_capture"},
		{"profile assignment", http.MethodPut, "/api/config/profile-assignments", `{"target":"tool:cline","profile":"codex-variant"}`, "codex-variant", "profile_assignment"},
		{"sandbox", http.MethodPut, "/api/terminal/sandbox/config", "", "t572-sandboxes", "terminal_sandbox_config"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, h := newManageServer(t)
			if c.name == "process enable" && !buildfeat.HasProcessobs {
				t.Skip("process observation is compiled out of this build (sbo_omit_processobs)")
			}
			allowAllProcessCapture(s)
			body := c.body
			if c.name == "sandbox" {
				body = `{"enabled":true,"backend":"bwrap","home_mode":"readonly","workspaces_dir":"` +
					filepath.ToSlash(filepath.Join(t.TempDir(), "t572-sandboxes")) + `","prep_timeout_seconds":90}`
			}
			before, _ := os.ReadFile(s.opts.ConfigPath)

			// 1. A dry run answers a diff and writes nothing.
			rec := sshProfilesReq(t, h, c.method, c.path+"?dry_run=1", body, true)
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
			after, _ := os.ReadFile(s.opts.ConfigPath)
			if string(after) != string(before) {
				t.Fatal("a dry run wrote config.toml")
			}

			// 2. The file changes after the preview: the save at the
			// previewed etag is refused and the outside edit survives.
			outside := string(before) + "\n# edited outside the page\n"
			if err := os.WriteFile(s.opts.ConfigPath, []byte(outside), 0o600); err != nil {
				t.Fatal(err)
			}
			rec = sshProfilesReq(t, h, c.method, c.path+"?base_etag="+dry.ConfigEtag, body, true)
			if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "config_changed") {
				t.Fatalf("stale save = %d: %s (want 409 config_changed)", rec.Code, rec.Body)
			}
			now, _ := os.ReadFile(s.opts.ConfigPath)
			if string(now) != outside {
				t.Fatal("a stale save overwrote the outside edit")
			}

			// 3. A save with no base etag still writes (older clients) and
			// leaves an audit row naming the action.
			rec = sshProfilesReq(t, h, c.method, c.path, body, true)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"saved":true`) {
				t.Fatalf("save = %d: %s", rec.Code, rec.Body)
			}
			written, _ := os.ReadFile(s.opts.ConfigPath)
			if !strings.Contains(string(written), c.wantInDiff) {
				t.Fatalf("saved config.toml lacks %q:\n%s", c.wantInDiff, written)
			}
			var n int
			if err := s.opts.DB.QueryRow(`SELECT COUNT(*) FROM remote_audit WHERE detail LIKE ?`, c.auditAction+"%").Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Fatalf("audit rows for %s = %d, want 1", c.auditAction, n)
			}
		})
	}
}
