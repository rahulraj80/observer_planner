//go:build !sbo_omit_integration_shell_wrap

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/shellwrap"
)

// TestShellWrapLaunchEnvBrokenConfig pins P3-09's second half: a config.toml
// that does not load used to answer SBO_OK (unrouted, every gate skipped) on
// every node. An org-governed node now refuses; an unmanaged node keeps the
// never-locked-out unrouted answer.
func TestShellWrapLaunchEnvBrokenConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[proxy\nport = = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := launchEnvDeps{
		refuseDisallowed: func(string, string, io.Writer) error { return nil },
		admit:            func(context.Context, string, string, budgetLaunchEvidence) error { return nil },
		reachable:        func(string, time.Duration) bool { return true },
		environ:          func() []string { return nil },
	}
	cases := []struct {
		name     string
		governed bool
		wantOut  string
		wantErr  string
	}{
		{"governed node refuses", true, "SBO_DENY\n", "governed by an organization"},
		{"unmanaged node runs unrouted", false, "SBO_OK\n", "running claude-code unrouted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := base
			governed := tc.governed
			deps.orgGovernedWithoutConfig = func() bool { return governed }
			var out, errb bytes.Buffer
			if err := runShellWrapLaunchEnv(context.Background(), &out, &errb, cfgPath, "claude-code", deps); err != nil {
				t.Fatal(err)
			}
			if out.String() != tc.wantOut || !strings.Contains(errb.String(), tc.wantErr) {
				t.Fatalf("stdout %q stderr %q, want %q / %q", out.String(), errb.String(), tc.wantOut, tc.wantErr)
			}
		})
	}
}

// TestDefaultOrgGovernedWithoutConfig: the config-free probe reads the two
// sidecars beside the DEFAULT database path (under HOME).
func TestDefaultOrgGovernedWithoutConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if defaultOrgGovernedWithoutConfig() {
		t.Fatal("governed with no sidecar")
	}
	dir := filepath.Join(home, ".observer")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"features-effective.json", "governance-effective.json"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if !defaultOrgGovernedWithoutConfig() {
			t.Fatalf("not governed with %s present", name)
		}
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
}

// TestEveryShimVerbAcceptsConfig: a shim now appends `--config <path>` to the
// verb it runs (P3-09), so every wrapped verb, on every host shape, and the
// bridge's `shell-wrap launch-env`, must register a --config flag.
func TestEveryShimVerbAcceptsConfig(t *testing.T) {
	requireCompiled(t, "terminals")
	root := newRootCmd()
	if lc, _, err := root.Find([]string{"shell-wrap", "launch-env"}); err != nil || lc.Flags().Lookup("config") == nil && lc.InheritedFlags().Lookup("config") == nil {
		t.Fatalf("shell-wrap launch-env takes no --config (%v)", err)
	}
	hosts := []shellwrap.Host{
		{GOOS: "linux", Home: "/h"},
		{GOOS: "darwin", Home: "/h"},
		{GOOS: "windows", Home: `C:\Users\u`},
		{GOOS: "windows", Home: `C:\Users\u`, Bridge: &shellwrap.WSLBridge{Distro: "Ubuntu"}},
	}
	seen := 0
	for _, h := range hosts {
		for _, c := range shellwrap.CandidatesOn(h) {
			if h.Bridge != nil && c.Side == shellwrap.SideHost {
				continue // runs launch-env, checked above
			}
			w, ok := integration.WrappedCommandFor(c.ID)
			if !ok {
				continue
			}
			cmd, _, err := root.Find(w.Args)
			if err != nil || cmd == root {
				t.Errorf("%s: `observer %s` is not a command", c.ID, strings.Join(w.Args, " "))
				continue
			}
			seen++
			if cmd.Flags().Lookup("config") == nil && cmd.InheritedFlags().Lookup("config") == nil {
				t.Errorf("%s: `observer %s` has no --config flag a shim can pass", c.ID, strings.Join(w.Args, " "))
			}
		}
	}
	if seen == 0 {
		t.Fatal("no wrapped verbs found")
	}
}
