package dashboard

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

// REVIEW-3 P3-18 (T-488 / T-489): the SSH profile editor and the launch
// policy editor rewrote config.toml directly, with no preview, no conflict
// check and no read-back. Both now go through the ONE save pipeline: a dry
// run previews the diff and writes nothing, and a save at a stale base etag
// is refused instead of overwriting an edit made since the preview.

func TestP318EditorsPreviewAndRefuseStaleEtag(t *testing.T) {
	cases := []struct {
		name, path, body, wantInDiff string
	}{
		{"ssh profiles", "/api/terminal/ssh/profiles", `{"enabled":true,"profiles":[{"name":"build","host":"10.0.0.7","port":2222}]}`, "10.0.0.7"},
		{"launch policy", "/api/terminal/policy", `{"allow_fresh_agent":true,"allowed_tools":[],"allowed_project_roots":[],"allow_shell":true}`, "allow_fresh_agent"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, h := newManageServer(t)
			before, _ := os.ReadFile(s.opts.ConfigPath)

			// 1. A dry run answers a diff and writes nothing.
			rec := sshProfilesReq(t, h, http.MethodPut, c.path+"?dry_run=1", c.body, true)
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
				t.Fatalf("dry run answer = %+v", dry)
			}
			after, _ := os.ReadFile(s.opts.ConfigPath)
			if string(after) != string(before) {
				t.Fatal("a dry run wrote config.toml")
			}

			// 2. The file changes after the preview: the save at the
			// previewed etag is refused, the outside edit survives.
			outside := string(before) + "\n# edited outside the page\n"
			if err := os.WriteFile(s.opts.ConfigPath, []byte(outside), 0o600); err != nil {
				t.Fatal(err)
			}
			rec = sshProfilesReq(t, h, http.MethodPut, c.path+"?base_etag="+dry.ConfigEtag, c.body, true)
			if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "config_changed") {
				t.Fatalf("stale save = %d: %s (want 409 config_changed)", rec.Code, rec.Body)
			}
			now, _ := os.ReadFile(s.opts.ConfigPath)
			if string(now) != outside {
				t.Fatal("a stale save overwrote the outside edit")
			}

			// 3. A save with no base etag still writes (older clients).
			rec = sshProfilesReq(t, h, http.MethodPut, c.path, c.body, true)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"saved":true`) {
				t.Fatalf("save = %d: %s", rec.Code, rec.Body)
			}
		})
	}
}
