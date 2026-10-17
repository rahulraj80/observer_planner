package dashboard

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/hook"
)

// TestHookProbeReadsDaemonRegistrationAsOn is the T-376 regression: after
// `observer start` auto-registers Claude Code's hooks (binary plus
// --config), Settings > Connected tools read them as off, because the
// probe dry-ran a registry built without --config and the registrar only
// counts an entry whose command matches exactly. The probe now dry-runs
// through the daemon's own registration options.
func TestHookProbeReadsDaemonRegistrationAsOn(t *testing.T) {
	home := t.TempDir()
	cfgPath := filepath.Join(home, ".observer", "config.toml")
	daemonOpts := hook.Options{
		BinaryPath: "/opt/observer/bin/observer",
		HomeDir:    home,
		ConfigPath: cfgPath,
	}
	reg, err := hook.NewRegistry(daemonOpts)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if res := reg.Register("claude-code"); res.Error != nil || len(res.HooksAdded) == 0 {
		t.Fatalf("daemon registration: err=%v added=%v", res.Error, res.HooksAdded)
	}
	settings := filepath.Join(home, ".claude", "settings.json")
	before, err := os.ReadFile(settings)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	cases := []struct {
		name string
		opts hook.Options
		want bool
	}{
		// The injected seam returns DryRun=false on purpose: the probe
		// must force it on.
		{"daemon options", daemonOpts, true},
		// The pre-fix probe shape (no --config) misreads the same file.
		{"probe without --config", hook.Options{BinaryPath: daemonOpts.BinaryPath, HomeDir: home}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts := c.opts
			s := &Server{opts: Options{HookRegistryOptions: func() (hook.Options, error) { return opts, nil }}}
			p := s.hookProber()("claude-code")
			if p.Registered != c.want {
				t.Errorf("Registered = %v, want %v (detail %q)", p.Registered, c.want, p.Detail)
			}
			after, err := os.ReadFile(settings)
			if err != nil {
				t.Fatalf("read settings: %v", err)
			}
			if !bytes.Equal(before, after) {
				t.Errorf("the probe wrote settings.json; it must stay a dry run")
			}
		})
	}
}
