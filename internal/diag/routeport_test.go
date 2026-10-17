package diag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// TestCheckRoutePort is U9: a Claude Code / Codex route left on :8820 after
// [proxy].port moved to 18820 warns with the forced route rewrite for each
// tool; a matching port is ok.
func TestCheckRoutePort(t *testing.T) {
	home := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".claude/settings.json", `{"env":{"ANTHROPIC_BASE_URL":"http://127.0.0.1:8820"}}`)
	write(".codex/config.toml", "model_provider = \"openai-observer\"\n\n[model_providers.openai-observer]\nname = \"x\"\nbase_url = \"http://127.0.0.1:8820/v1\"\n")
	var cfg config.Config
	cfg.Proxy.Port = 18820
	c := checkRoutePort(home, cfg)
	if c.Status != StatusWarn {
		t.Fatalf("status %s, want warn: %q", c.Status, c.Message)
	}
	for _, want := range []string{
		"observer init --claude-code --skip-hooks --skip-mcp --proxy-port 18820 --force",
		"observer init --codex --skip-hooks --skip-mcp --proxy-port 18820 --force",
	} {
		if !strings.Contains(c.Message, want) {
			t.Errorf("message %q lacks %q", c.Message, want)
		}
	}
	cfg.Proxy.Port = 8820
	if c := checkRoutePort(home, cfg); c.Status != StatusOK {
		t.Fatalf("matching port: %s %q", c.Status, c.Message)
	}
}
