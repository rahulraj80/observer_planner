package dashboard

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestConfigSection_KeepsComments pins T-524: a whole-section save (PUT
// /api/config/section/<id>) changes only the keys the save changed, through
// the comment-preserving editor, instead of re-serializing config.toml. Before
// the fix every section save dropped the operator's comments and wrote every
// default value into the file. One case per value shape: a scalar section,
// and a section whose save changes a list.
func TestConfigSection_KeepsComments(t *testing.T) {
	cases := []struct {
		name    string
		section string
		body    string
		// want are lines (or fragments) the saved file must contain.
		want []string
		// check decodes the saved file and checks the changed values.
		check func(t *testing.T, raw map[string]any)
	}{
		{
			name:    "scalar section",
			section: "intelligence",
			body:    `{"MonthlyBudgetUSD": 75, "APIKeyEnv": "", "SummaryModel": ""}`,
			want:    []string{"monthly_budget_usd = 75"},
			check: func(t *testing.T, raw map[string]any) {
				in, _ := raw["intelligence"].(map[string]any)
				if v, _ := in["monthly_budget_usd"].(int64); v != 75 {
					if f, _ := in["monthly_budget_usd"].(float64); f != 75 {
						t.Fatalf("intelligence.monthly_budget_usd = %v", in["monthly_budget_usd"])
					}
				}
			},
		},
		{
			name:    "section with a list",
			section: "watcher",
			body:    `{"PollIntervalSeconds": 7, "MaxFileSizeMB": 64, "EnabledAdapters": ["claude-code", "codex"]}`,
			want:    []string{"poll_interval_seconds = 7"},
			check: func(t *testing.T, raw map[string]any) {
				obs, _ := raw["observer"].(map[string]any)
				w, _ := obs["watch"].(map[string]any)
				if w["poll_interval_seconds"] != int64(7) || w["max_file_size_mb"] != int64(64) {
					t.Fatalf("observer.watch = %v", w)
				}
				list, _ := w["enabled_adapters"].([]any)
				if len(list) != 2 || list[0] != "claude-code" || list[1] != "codex" {
					t.Fatalf("observer.watch.enabled_adapters = %v", w["enabled_adapters"])
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, path := newSecretTestServer(t, keysCorpus)
			rr := putSection(t, s, c.section, c.body, true)
			if rr.Code != http.StatusOK {
				t.Fatalf("save: %d %s", rr.Code, rr.Body.String())
			}
			m := decodeMap(t, rr)
			if m["write_mode"] != "surgical" || m["comments_preserved"] != true {
				t.Fatalf("a section save must keep comments: write_mode=%v comments_preserved=%v reason=%v",
					m["write_mode"], m["comments_preserved"], m["write_mode_reason"])
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			text := string(got)
			for _, keep := range []string{
				"# Operator header comment - must survive.",
				"# log_level explains itself",
				`log_level = "warn" # inline: keep me`,
				`allowed_tools = ["claude"]`,
			} {
				if !strings.Contains(text, keep) {
					t.Fatalf("section save lost %q:\n%s", keep, text)
				}
			}
			for _, w := range c.want {
				if !strings.Contains(text, w) {
					t.Fatalf("section save did not write %q:\n%s", w, text)
				}
			}
			// Only the changed keys were added: no wall of defaults.
			if n := strings.Count(text, "\n"); n > strings.Count(keysCorpus, "\n")+8 {
				t.Fatalf("section save grew the file from %d to %d lines (defaults written out?):\n%s",
					strings.Count(keysCorpus, "\n"), n, text)
			}
			raw := map[string]any{}
			if _, err := toml.Decode(text, &raw); err != nil {
				t.Fatalf("saved file does not decode: %v\n%s", err, text)
			}
			c.check(t, raw)
		})
	}
}
