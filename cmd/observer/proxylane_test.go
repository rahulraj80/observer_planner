package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// TestEffectiveAgentUpstreamsCarriesTheBuiltInLanes (Sol A5-02 follow-up):
// a fresh install's proxy resolves the lanes its launchers address (grok,
// and hermes' default openrouter) from the registry, keeps the config's own
// opencode-zen default, and lets [proxy.agent_upstreams] override a built-in.
func TestEffectiveAgentUpstreamsCarriesTheBuiltInLanes(t *testing.T) {
	cfg := config.Default()
	eff := effectiveAgentUpstreams(cfg)
	for lane, want := range map[string]string{
		"grok":                 "https://cli-chat-proxy.grok.com",
		"openrouter":           "https://openrouter.ai",
		config.OpenCodeZenLane: "https://opencode.ai/zen",
	} {
		if eff[lane] != want {
			t.Errorf("lane %q = %q, want %q", lane, eff[lane], want)
		}
	}
	cfg.Proxy.AgentUpstreams = map[string]string{"grok": "https://grok.override.example"}
	if got := effectiveAgentUpstreams(cfg)["grok"]; got != "https://grok.override.example" {
		t.Errorf("configured grok lane = %q, want the [proxy.agent_upstreams] override", got)
	}
}

// TestRequireLauncherLane pins the preflight, one row per resolution source.
func TestRequireLauncherLane(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*config.Config)
		lane   string
		ok     bool
	}{
		{"built-in grok lane", func(*config.Config) {}, "grok", true},
		{"built-in openrouter lane", func(*config.Config) {}, "openrouter", true},
		{"a [proxy.upstreams] lane", func(c *config.Config) {
			c.Proxy.Upstreams = map[string]string{"together": "https://api.together.xyz"}
		}, "together", true},
		{"a [proxy.agent_upstreams] lane", func(c *config.Config) {
			c.Proxy.AgentUpstreams = map[string]string{"mine": "https://example.test"}
		}, "mine", true},
		{"an unconfigured lane", func(*config.Config) {}, "nowhere", false},
		{"a built-in lane disabled in [proxy.agent_upstreams]", func(c *config.Config) {
			c.Proxy.AgentUpstreams = map[string]string{"grok": ""}
		}, "grok", false},
		{"no lane needed", func(*config.Config) {}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			tc.mutate(&cfg)
			err := requireLauncherLane(cfg, "test", tc.lane, "launch without --proxy")
			if (err == nil) != tc.ok {
				t.Fatalf("requireLauncherLane(%q) = %v, want ok=%v", tc.lane, err, tc.ok)
			}
			if err != nil && (!strings.Contains(err.Error(), "[proxy.upstreams]") || !strings.Contains(err.Error(), "launch without --proxy")) {
				t.Errorf("refusal does not say how to fix it: %v", err)
			}
		})
	}
	if err := requireRouteLane(config.Default(), "grok", "grok", "launch without --proxy"); err != nil {
		t.Errorf("grok's registry lane does not resolve on a fresh install: %v", err)
	}
}

// TestGrokProxyLaunchRefusesAnUnresolvableLane: `observer grok --proxy` with
// its lane disabled is refused before grok starts, instead of starting grok
// against a proxy that would answer every request with a 502.
func TestGrokProxyLaunchRefusesAnUnresolvableLane(t *testing.T) {
	originalRuntimeDir := agentRuntimeDir
	originalLoginPathDirs := daemonLoginPathDirs
	agentRuntimeDir = func() string { return "" }
	daemonLoginPathDirs = func() []string { return nil }
	t.Cleanup(func() {
		agentRuntimeDir = originalRuntimeDir
		daemonLoginPathDirs = originalLoginPathDirs
	})

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	body := "[observer]\ndb_path = \"" + filepath.ToSlash(filepath.Join(dir, "observer.db")) + "\"\n\n[proxy.agent_upstreams]\ngrok = \"\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "vendor-started")
	stub := filepath.Join(dir, "grok-stub")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\ntouch \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	output := new(bytes.Buffer)
	root := &cobra.Command{Use: "observer", SilenceUsage: true}
	root.AddCommand(newGrokCmd())
	root.SetArgs([]string{"grok", "--config", cfgPath, "--grok-path", stub, "--no-attach", "--proxy", "--", marker})
	root.SetOut(output)
	root.SetErr(output)
	executed, err := root.ExecuteC()
	if err == nil || !strings.Contains(err.Error(), `lane "grok"`) {
		t.Fatalf("observer grok --proxy = %v, want the lane refusal", err)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("grok started despite the refusal: %v", statErr)
	}
	// The launcher silences Cobra, so the refusal must reach the terminal
	// through main's bounded-message seam, or the operator sees a bare exit 1
	// (found live in Integration 4).
	terminal := new(bytes.Buffer)
	if !renderSuppressedLauncherError(terminal, root, executed, err) || !strings.Contains(terminal.String(), `lane "grok"`) ||
		!strings.Contains(terminal.String(), "launch without --proxy") {
		t.Fatalf("the lane refusal was not shown on the terminal: %q", terminal.String())
	}
}

// TestRequireLauncherLaneAsksTheRunningDaemon pins T-308: when the daemon's
// proxy answers /-/lanes, ITS live table decides (an org-distributed
// gateway.providers lane that no config names resolves; a config lane the
// org table replaced does not), and only when no daemon answers does the
// preflight fall back to config and the built-ins.
func TestRequireLauncherLaneAsksTheRunningDaemon(t *testing.T) {
	orig := daemonLanes
	t.Cleanup(func() { daemonLanes = orig })
	cases := []struct {
		name    string
		live    map[string]bool // nil = no daemon answers
		cfgLane string          // a [proxy.upstreams] lane in config
		lane    string
		ok      bool
		daemon  bool // the refusal names the running daemon
	}{
		{"org lane only the daemon knows", map[string]bool{"org-gw": true}, "", "org-gw", true, false},
		{"config lane the live table replaced", map[string]bool{"org-gw": true}, "together", "together", false, true},
		{"no daemon: config lane resolves", nil, "together", "together", true, false},
		{"no daemon: unknown lane refused", nil, "", "org-gw", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			daemonLanes = func(string) (map[string]bool, bool) { return tc.live, tc.live != nil }
			cfg := config.Default()
			if tc.cfgLane != "" {
				cfg.Proxy.Upstreams = map[string]string{tc.cfgLane: "https://example.test"}
			}
			err := requireLauncherLane(cfg, "test", tc.lane, "launch without --proxy")
			if (err == nil) != tc.ok {
				t.Fatalf("requireLauncherLane(%q) = %v, want ok=%v", tc.lane, err, tc.ok)
			}
			if err != nil && strings.Contains(err.Error(), "running observer daemon") != tc.daemon {
				t.Errorf("refusal source wrong (want daemon=%v): %v", tc.daemon, err)
			}
		})
	}
}

// TestDaemonLanesProbe pins the probe's contract against a stand-in proxy:
// a well-formed /-/lanes body is read; a 404 (an older daemon without the
// endpoint) or a body without "lanes" counts as no answer.
func TestDaemonLanesProbe(t *testing.T) {
	orig := daemonLanes
	t.Cleanup(func() { daemonLanes = orig })
	daemonLanes = probeDaemonLanes
	for _, tc := range []struct {
		name   string
		status int
		body   string
		ok     bool
		has    string
	}{
		{"lanes body", 200, `{"lanes":["grok","org-gw"]}`, true, "org-gw"},
		{"empty lanes is an answer", 200, `{"lanes":[]}`, true, ""},
		{"older daemon 404", 404, `not found`, false, ""},
		{"no lanes field", 200, `{"status":"ok"}`, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/-/lanes" || r.Method != http.MethodGet {
					t.Errorf("probe hit %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			got, ok := daemonLanes(srv.URL)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if tc.has != "" && !got[tc.has] {
				t.Errorf("lanes %v missing %q", got, tc.has)
			}
		})
	}
}
