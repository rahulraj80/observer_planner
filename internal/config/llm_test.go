package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/llmconn"
)

// TestLLMDefaultsOff pins the opt-in invariant: the zero [llm] section yields
// no connections (and thus no outbound path).
func TestLLMDefaultsOff(t *testing.T) {
	cfg := Default()
	if len(cfg.LLM.Connections) != 0 {
		t.Fatalf("Default LLM connections = %v, want none", cfg.LLM.Connections)
	}
	if got := cfg.LLM.ToConnections(); len(got) != 0 {
		t.Fatalf("ToConnections() = %v, want empty", got)
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
}

// TestLLMConnectionsLoadAndConvert exercises the static TOML → runtime
// conversion (both dialects + loopback/remote + roles) through Load.
func TestLLMConnectionsLoadAndConvert(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	body := `
[llm.connections.local-ollama]
name = "Local Ollama"
kind = "local_openai_compat"
base_url = "http://127.0.0.1:11434/v1"
enabled = true
default_model = "qwen2.5:7b"
task_roles = ["narrative", "ask"]

[llm.connections.remote-anthropic]
name = "Remote Anthropic"
kind = "anthropic"
base_url = "https://api.anthropic.com"
api_key_env = "ANTHROPIC_API_KEY"
allow_remote = true
enabled = true
default_model = "claude-haiku-4-5-20251001"
task_roles = ["ask"]
`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(LoadOptions{GlobalPath: p, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	conns := cfg.LLM.ToConnections()
	if len(conns) != 2 {
		t.Fatalf("ToConnections() = %d entries, want 2", len(conns))
	}
	// Sorted by id: local-ollama before remote-anthropic.
	if conns[0].ID != "local-ollama" || conns[1].ID != "remote-anthropic" {
		t.Fatalf("order = %s,%s — want local-ollama,remote-anthropic", conns[0].ID, conns[1].ID)
	}
	local := conns[0]
	if !local.Local() || local.Enabled != true || local.DefaultModel != "qwen2.5:7b" {
		t.Errorf("local conn wrong: %+v", local)
	}
	if len(local.TaskRoles) != 2 || local.TaskRoles[0] != llmconn.TaskNarrative || local.TaskRoles[1] != llmconn.TaskAsk {
		t.Errorf("local roles = %v", local.TaskRoles)
	}
	remote := conns[1]
	if remote.Local() || !remote.AllowRemote || remote.APIKeyEnv != "ANTHROPIC_API_KEY" || remote.Kind != llmconn.KindAnthropic {
		t.Errorf("remote conn wrong: %+v", remote)
	}
}

func TestValidateLLM(t *testing.T) {
	valid := LLMConfig{Connections: map[string]LLMConnectionConfig{
		"local":  {Kind: "local_openai_compat", BaseURL: "http://127.0.0.1:11434/v1", Enabled: true},
		"remote": {Kind: "openai_compat", BaseURL: "https://api.example.com", APIKeyEnv: "OPENAI_API_KEY", AllowRemote: true, Enabled: true},
	}}
	if err := validateLLM(valid); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	tests := []struct {
		name string
		mut  func(*LLMConfig)
		want string
	}{
		{"empty id", func(c *LLMConfig) {
			delete(c.Connections, "local")
			delete(c.Connections, "remote")
			c.Connections[""] = LLMConnectionConfig{BaseURL: "http://x"}
		}, "id must not be empty"},
		{"bad kind", func(c *LLMConfig) { v := c.Connections["local"]; v.Kind = "gemini"; c.Connections["local"] = v }, "not in"},
		{"empty base_url", func(c *LLMConfig) { v := c.Connections["local"]; v.BaseURL = " "; c.Connections["local"] = v }, "base_url is required"},
		{"bad base_url", func(c *LLMConfig) { v := c.Connections["local"]; v.BaseURL = "://nope"; c.Connections["local"] = v }, "not a valid URL"},
		{"bad task role", func(c *LLMConfig) {
			v := c.Connections["local"]
			v.TaskRoles = []string{"teleport"}
			c.Connections["local"] = v
		}, "not in"},
		{"negative max_tokens", func(c *LLMConfig) { v := c.Connections["local"]; v.MaxTokens = -1; c.Connections["local"] = v }, "must be >= 0"},
		{"literal secret in env ref", func(c *LLMConfig) {
			v := c.Connections["remote"]
			v.APIKeyEnv = "sk-abc123"
			c.Connections["remote"] = v
		}, "never the literal key"},
		{"literal secret in keychain ref", func(c *LLMConfig) {
			v := c.Connections["remote"]
			v.APIKeyKeychain = "AIzaSy..."
			c.Connections["remote"] = v
		}, "never the literal key"},
		{"enabled remote missing allow_remote", func(c *LLMConfig) { v := c.Connections["remote"]; v.AllowRemote = false; c.Connections["remote"] = v }, "allow_remote=true"},
		{"enabled remote missing key ref", func(c *LLMConfig) { v := c.Connections["remote"]; v.APIKeyEnv = ""; c.Connections["remote"] = v }, "api_key_env or api_key_keychain"},
		{"header empty name", func(c *LLMConfig) {
			v := c.Connections["local"]
			v.Headers = map[string]string{"": "x"}
			c.Connections["local"] = v
		}, "must not be empty"},
		{"header invalid name", func(c *LLMConfig) {
			v := c.Connections["local"]
			v.Headers = map[string]string{"X A: B": "x"}
			c.Connections["local"] = v
		}, "invalid characters"},
		{"header CRLF value", func(c *LLMConfig) {
			v := c.Connections["local"]
			v.Headers = map[string]string{"X-Foo": "a\r\nInjected: 1"}
			c.Connections["local"] = v
		}, "must not contain CR/LF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := LLMConfig{Connections: map[string]LLMConnectionConfig{}}
			for k, v := range valid.Connections {
				c.Connections[k] = v
			}
			tt.mut(&c)
			err := validateLLM(c)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

// TestValidateLLMAcceptsAskPastAndCoachRoles pins that the ask_past and coach
// task roles are first-class vocabulary (they were missing at first landing —
// a connection restricted to either role was rejected at load).
func TestValidateLLMAcceptsAskPastAndCoachRoles(t *testing.T) {
	c := LLMConfig{Connections: map[string]LLMConnectionConfig{
		"past": {
			Kind:         "local_openai_compat",
			BaseURL:      "http://127.0.0.1:11434/v1",
			Enabled:      true,
			TaskRoles:    []string{"ask_past"},
			DefaultModel: "qwen2.5:7b",
		},
		"coach": {
			Kind:         "local_openai_compat",
			BaseURL:      "http://127.0.0.1:11434/v1",
			Enabled:      true,
			TaskRoles:    []string{"coach"},
			DefaultModel: "qwen2.5:7b",
		},
	}}
	if err := validateLLM(c); err != nil {
		t.Fatalf("ask_past/coach task roles rejected: %v", err)
	}
	conns := c.ToConnections()
	if len(conns) != 2 {
		t.Fatalf("conns = %d, want 2", len(conns))
	}
	byID := map[string]llmconn.Connection{}
	for _, cc := range conns {
		byID[cc.ID] = cc
	}
	if len(byID["past"].TaskRoles) != 1 || byID["past"].TaskRoles[0] != llmconn.TaskAskPast {
		t.Fatalf("past roles = %v", byID["past"].TaskRoles)
	}
	if len(byID["coach"].TaskRoles) != 1 || byID["coach"].TaskRoles[0] != llmconn.TaskCoach {
		t.Fatalf("coach roles = %v", byID["coach"].TaskRoles)
	}
}

// TestLLMHeadersRoundTrip pins that static Headers survive the config → runtime
// conversion (and that a benign extra header validates).
func TestLLMHeadersRoundTrip(t *testing.T) {
	c := LLMConfig{Connections: map[string]LLMConnectionConfig{
		"gw": {
			Kind:         "openai_compat",
			BaseURL:      "http://localhost:3001/v1",
			APIKeyEnv:    "GW_KEY",
			DefaultModel: "openai/gpt-oss-20b:free",
			Enabled:      true,
			Headers:      map[string]string{"X-API-Key": "custom", "X-Routing": "north"},
		},
	}}
	if err := validateLLM(c); err != nil {
		t.Fatalf("valid headers rejected: %v", err)
	}
	conns := c.ToConnections()
	if len(conns) != 1 {
		t.Fatalf("conns = %d", len(conns))
	}
	got := conns[0]
	if got.Headers["X-API-Key"] != "custom" || got.Headers["X-Routing"] != "north" {
		t.Errorf("headers = %+v", got.Headers)
	}
	if got.DefaultModel != "openai/gpt-oss-20b:free" {
		t.Errorf("default_model = %q", got.DefaultModel)
	}
}

// TestLLMDisabledRemoteInert asserts a remote connection that is NOT enabled
// needs no allow_remote or key ref (mid-setup configs stay loadable).
func TestLLMDisabledRemoteInert(t *testing.T) {
	c := LLMConfig{Connections: map[string]LLMConnectionConfig{
		"draft": {Kind: "openai_compat", BaseURL: "https://api.example.com"},
	}}
	if err := validateLLM(c); err != nil {
		t.Fatalf("disabled remote should validate: %v", err)
	}
}

func TestConnectionsSorted(t *testing.T) {
	c := LLMConfig{Connections: map[string]LLMConnectionConfig{
		"zeta":  {BaseURL: "http://127.0.0.1:9"},
		"alpha": {BaseURL: "http://127.0.0.1:9"},
		"mid":   {BaseURL: "http://127.0.0.1:9"},
	}}
	conns := c.ToConnections()
	for i := 1; i < len(conns); i++ {
		if conns[i-1].ID >= conns[i].ID {
			t.Fatalf("not sorted: %s before %s", conns[i-1].ID, conns[i].ID)
		}
	}
}
