package main

import (
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// TestHookProbeOptionsMirrorAutoRegister pins T-376 at the cmd seam: the
// dashboard's Connected tools hooks probe dry-runs with exactly the
// options `observer start`'s auto-registration writes with (same binary,
// same resolved --config, same owes-live filter), only with DryRun on.
func TestHookProbeOptionsMirrorAutoRegister(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	write, err := autoRegisterOptions(cfgPath)
	if err != nil {
		t.Fatalf("autoRegisterOptions: %v", err)
	}
	probe, err := hookProbeOptionsSeam(cfgPath)()
	if err != nil {
		t.Fatalf("hookProbeOptionsSeam: %v", err)
	}
	if !probe.DryRun {
		t.Fatal("probe options must be a dry run")
	}
	if write.DryRun {
		t.Fatal("auto-registration options must not be a dry run")
	}
	resolved, _ := config.ResolveGlobalPath(cfgPath)
	if probe.ConfigPath != resolved || write.ConfigPath != resolved {
		t.Errorf("ConfigPath probe=%q write=%q, want both %q", probe.ConfigPath, write.ConfigPath, resolved)
	}
	if probe.BinaryPath != write.BinaryPath || probe.BinaryPath == "" {
		t.Errorf("BinaryPath probe=%q write=%q", probe.BinaryPath, write.BinaryPath)
	}
	if probe.WSLDistro != write.WSLDistro {
		t.Errorf("WSLDistro probe=%q write=%q", probe.WSLDistro, write.WSLDistro)
	}
	if probe.SkipEvent == nil || write.SkipEvent == nil {
		t.Fatal("both must carry the owes-live filter")
	}
	for key := range autoRegisterOwesLive {
		tool, event, _ := cutLast(key)
		if !probe.SkipEvent(tool, event) {
			t.Errorf("probe SkipEvent(%q, %q) = false; the probe would expect a binding start never writes", tool, event)
		}
	}
}

// cutLast splits "tool Event" at its last space.
func cutLast(key string) (string, string, bool) {
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == ' ' {
			return key[:i], key[i+1:], true
		}
	}
	return key, "", false
}
