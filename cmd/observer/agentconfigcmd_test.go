package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgentConfigCLIFailsClosedOnUnreadableDB pins review M3-15: a node
// database that EXISTS but cannot be opened leaves the editor read-only
// (saying why) instead of treating the machine as unmanaged; a machine with
// no database at all stays editable.
func TestAgentConfigCLIFailsClosedOnUnreadableDB(t *testing.T) {
	cases := []struct {
		name     string
		seed     func(path string) error
		readOnly bool
	}{
		{"no database yet", func(string) error { return nil }, false},
		{"database unreadable", func(p string) error {
			return os.WriteFile(p, []byte("this is not a sqlite database at all, just text"), 0o600)
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			dbPath := filepath.Join(home, "observer.db")
			if err := c.seed(dbPath); err != nil {
				t.Fatal(err)
			}
			cfgPath := filepath.Join(home, "observer-config.toml")
			if err := os.WriteFile(cfgPath, []byte("[observer]\ndb_path = \""+filepath.ToSlash(dbPath)+"\"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			env, err := openAgentConfig(context.Background(), cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			defer env.cleanup()
			if env.access.ReadOnly != c.readOnly {
				t.Fatalf("access = %+v, want read_only=%v", env.access, c.readOnly)
			}
			if c.readOnly && !strings.Contains(env.access.Reason, "cannot read this machine's organization posture") {
				t.Fatalf("reason = %q", env.access.Reason)
			}
		})
	}
}

// The `observer agent-config` CLI drives the same service as the dashboard:
// a throwaway HOME with a fake Claude Code settings file, never a real one.
func TestAgentConfigCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	settings := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	const secret = "plain-cli-secret-value"
	if err := os.WriteFile(settings, []byte(`{"model":"sonnet","env":{"OPENAI_API_KEY":"`+secret+`"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(home, "observer-config.toml")
	if err := os.WriteFile(cfgPath, []byte("[observer]\ndb_path = \""+filepath.ToSlash(filepath.Join(home, "none.db"))+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, error) {
		cmd := newAgentConfigCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(append(args, "--config", cfgPath))
		err := cmd.Execute()
		return out.String(), err
	}

	out, err := run("inspect", "--tool", "claude-code")
	if err != nil || !strings.Contains(out, "claude-code/user-settings") || strings.Contains(out, secret) {
		t.Fatalf("inspect: %v\n%s", err, out)
	}
	out, err = run("set", "claude-code", "model", "opus", "--dry-run")
	if err != nil || !strings.Contains(out, "dry run") || !strings.Contains(out, `"opus"`) || strings.Contains(out, secret) {
		t.Fatalf("set --dry-run: %v\n%s", err, out)
	}
	if b, _ := os.ReadFile(settings); strings.Contains(string(b), "opus") {
		t.Fatal("a dry run wrote the file")
	}
	out, err = run("set", "claude-code", "model", "opus")
	if err != nil || !strings.Contains(out, "saved") {
		t.Fatalf("set: %v\n%s", err, out)
	}
	b, _ := os.ReadFile(settings)
	if !strings.Contains(string(b), `"opus"`) || !strings.Contains(string(b), secret) {
		t.Fatalf("written file: %s", b)
	}
	if _, err := run("set", "claude-code", "no.such.setting", "1"); err == nil {
		t.Fatal("an unknown setting must be refused")
	}
	out, err = run("coverage")
	if err != nil || !strings.Contains(out, "claude-code") {
		t.Fatalf("coverage: %v\n%s", err, out)
	}
}
