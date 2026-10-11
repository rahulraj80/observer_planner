package diag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckHooksDisabled(t *testing.T) {
	cases := []struct {
		name     string
		files    map[string]string
		want     Status
		contains string
	}{
		{"clean home", nil, StatusOK, "no vendor switch"},
		{"claude disableAllHooks", map[string]string{".claude/settings.json": `{"disableAllHooks":true}`}, StatusWarn, "grok"},
		{"claude hooks on", map[string]string{".claude/settings.json": `{"disableAllHooks":false,"hooks":{}}`}, StatusOK, "no vendor switch"},
		{"gemini disabled", map[string]string{".gemini/settings.json": `{"hooksConfig":{"enabled":false}}`}, StatusWarn, "gemini-cli"},
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
			c := checkHooksDisabled(home, home)
			if c.Status != tc.want {
				t.Fatalf("status = %v, want %v (%s)", c.Status, tc.want, c.Message)
			}
			if !strings.Contains(c.Message, tc.contains) {
				t.Fatalf("message %q lacks %q", c.Message, tc.contains)
			}
		})
	}
}
