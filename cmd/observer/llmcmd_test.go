package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
)

func runLLMCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	orig := pokeReload
	pokeReload = func() bool { return false }
	t.Cleanup(func() { pokeReload = orig })
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"llm"}, args...))
	err := root.Execute()
	return out.String(), err
}

// TestLLMAddListRemoveRoundTrip pins the `observer llm` front door: add lands
// in config.toml through the shared write owner, round-trips through
// config.Load, appears in list, and remove deletes it.
func TestLLMAddListRemoveRoundTrip(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")

	if out, err := runLLMCmd(t, "list", "--config", cfgPath); err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	} else if !strings.Contains(out, "no LLM analysis connections") {
		t.Fatalf("empty list should say so:\n%s", out)
	}

	if _, err := runLLMCmd(t, "add", "--config", cfgPath,
		"--id", "local-ollama",
		"--kind", "local_openai_compat",
		"--base-url", "http://127.0.0.1:11434/v1",
		"--default-model", "qwen3:8b"); err != nil {
		t.Fatalf("add: %v", err)
	}

	reloaded, err := config.Load(config.LoadOptions{GlobalPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	conn, ok := reloaded.LLM.Connections["local-ollama"]
	if !ok {
		t.Fatalf("connection not persisted: %+v", reloaded.LLM)
	}
	if conn.Kind != "local_openai_compat" || conn.BaseURL != "http://127.0.0.1:11434/v1" || conn.DefaultModel != "qwen3:8b" || !conn.Enabled {
		t.Fatalf("persisted connection wrong: %+v", conn)
	}

	out, err := runLLMCmd(t, "list", "--config", cfgPath)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "local-ollama") {
		t.Fatalf("list missing the added connection:\n%s", out)
	}

	if _, err := runLLMCmd(t, "remove", "local-ollama", "--config", cfgPath); err != nil {
		t.Fatalf("remove: %v", err)
	}
	reloaded, _ = config.Load(config.LoadOptions{GlobalPath: cfgPath})
	if _, ok := reloaded.LLM.Connections["local-ollama"]; ok {
		t.Fatal("connection still present after remove")
	}
}

// TestLLMAddRefusesRemoteWithoutOptIn pins the local-first invariant at the
// CLI: an enabled remote connection without allow_remote=true is refused
// before it can be written.
func TestLLMAddRefusesRemoteWithoutOptIn(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if _, err := runLLMCmd(t, "add", "--config", cfgPath,
		"--id", "openrouter",
		"--kind", "openai_compat",
		"--base-url", "https://openrouter.ai/api/v1",
		"--api-key-env", "OPENROUTER_API_KEY"); err == nil {
		t.Fatal("enabled remote without allow_remote must be refused")
	}
}

// TestLLMSlugFromURL pins the setup id derivation.
func TestLLMSlugFromURL(t *testing.T) {
	cases := map[string]string{
		"http://127.0.0.1:11434/v1": "local-127-0-0-1-11434",
		"http://127.0.0.1:8000/v1":  "local-127-0-0-1-8000",
		"http://localhost:11434/v1": "local-localhost-11434",
	}
	for in, want := range cases {
		if got := slugFromURL(in); got != want {
			t.Errorf("slugFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}
