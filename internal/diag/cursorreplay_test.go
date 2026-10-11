package diag

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckCursorClaudeReplay(t *testing.T) {
	claude := `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"/usr/bin/observer hook claude-code pre-tool"}]}]}}`
	cursor := `{"version":1,"hooks":{"stop":[{"command":"/usr/bin/observer hook cursor stop"}]}}`
	cases := []struct {
		name  string
		files map[string]string
		want  Status
	}{
		{"neither", nil, StatusOK},
		{"claude only", map[string]string{".claude/settings.json": claude}, StatusOK},
		{"cursor only", map[string]string{".cursor/hooks.json": cursor}, StatusOK},
		{"both", map[string]string{".claude/settings.json": claude, ".cursor/hooks.json": cursor}, StatusWarn},
		{"user hooks only", map[string]string{".claude/settings.json": `{"hooks":{}}`, ".cursor/hooks.json": cursor}, StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			for rel, body := range tc.files {
				p := filepath.Join(home, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if c := checkCursorClaudeReplay(home); c.Status != tc.want {
				t.Fatalf("status = %v, want %v (%s)", c.Status, tc.want, c.Message)
			}
		})
	}
}
