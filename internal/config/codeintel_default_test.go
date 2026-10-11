package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCodeIntelOptInDefault pins operator ruling D-307 (2026-10-05): code
// intelligence is OFF by default and runs only when the user turns it on.
// The partial-merge rule still holds: an explicit `enabled = true` keeps it
// on, and a partial [codeintel] section that never names `enabled` inherits
// the new default (off) while its other keys still apply.
func TestCodeIntelOptInDefault(t *testing.T) {
	t.Parallel()

	if Default().CodeIntel.Enabled {
		t.Fatal("Default().CodeIntel.Enabled = true, want false (code intelligence is opt-in, D-307)")
	}

	tests := []struct {
		name          string
		toml          string // "" = no config file at all
		wantEnabled   bool
		wantRetention int
		wantOnStart   bool
	}{
		{
			name:          "no config file is off",
			toml:          "",
			wantEnabled:   false,
			wantRetention: 90,
			wantOnStart:   true,
		},
		{
			name:          "explicit true stays on",
			toml:          "[codeintel]\nenabled = true\n",
			wantEnabled:   true,
			wantRetention: 90,
			wantOnStart:   true,
		},
		{
			name:          "explicit false is off",
			toml:          "[codeintel]\nenabled = false\n",
			wantEnabled:   false,
			wantRetention: 90,
			wantOnStart:   true,
		},
		{
			name:          "partial section without enabled is off and keeps its own keys",
			toml:          "[codeintel]\nretention_days = 7\n\n[codeintel.index]\non_start = false\n",
			wantEnabled:   false,
			wantRetention: 7,
			wantOnStart:   false,
		},
		{
			name:          "unrelated sections leave it off",
			toml:          "[observer]\nlog_level = \"info\"\n",
			wantEnabled:   false,
			wantRetention: 90,
			wantOnStart:   true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.toml")
			if tc.toml != "" {
				if err := os.WriteFile(path, []byte(tc.toml), 0o600); err != nil {
					t.Fatalf("write: %v", err)
				}
			}
			cfg, err := Load(LoadOptions{GlobalPath: path, Env: func(string) string { return "" }, GovernanceSidecar: NoGovernanceSidecar})
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.CodeIntel.Enabled != tc.wantEnabled {
				t.Errorf("codeintel.enabled = %v, want %v", cfg.CodeIntel.Enabled, tc.wantEnabled)
			}
			if cfg.CodeIntel.RetentionDays != tc.wantRetention {
				t.Errorf("codeintel.retention_days = %d, want %d", cfg.CodeIntel.RetentionDays, tc.wantRetention)
			}
			if cfg.CodeIntel.Index.OnStart != tc.wantOnStart {
				t.Errorf("codeintel.index.on_start = %v, want %v", cfg.CodeIntel.Index.OnStart, tc.wantOnStart)
			}
		})
	}
}

// TestCodeIntelManagedPostureFollowsDefault: on a managed node the governance
// overlay only changes codeintel.enabled when the org pins it (the key stays
// governanceDirFree, so both directions are legitimate fleet postures).
// Without a pin the node follows the new default (off); a pin of true turns
// it on, and a pin of false keeps it off even over a local `enabled = true`.
func TestCodeIntelManagedPostureFollowsDefault(t *testing.T) {
	tests := []struct {
		name    string
		local   string
		sidecar string
		want    bool
	}{
		{name: "governed node, no codeintel pin", local: "", sidecar: `{"schema":1,"state":"applied","pinned":{"guard.enabled":true}}`, want: false},
		{name: "org pins it on", local: "", sidecar: `{"schema":1,"state":"applied","pinned":{"codeintel.enabled":true}}`, want: true},
		{name: "org pins it off over a local opt-in", local: "[codeintel]\nenabled = true\n", sidecar: `{"schema":1,"state":"applied","pinned":{"codeintel.enabled":false}}`, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dbDir := t.TempDir()
			cfgPath := govTempConfig(t, dbDir, tc.local)
			govWriteSidecar(t, dbDir, tc.sidecar)
			cfg, _, err := LoadGovernance(LoadOptions{GlobalPath: cfgPath, Env: noEnv})
			if err != nil {
				t.Fatalf("LoadGovernance: %v", err)
			}
			if cfg.CodeIntel.Enabled != tc.want {
				t.Errorf("codeintel.enabled = %v, want %v", cfg.CodeIntel.Enabled, tc.want)
			}
		})
	}
}

// TestFileDefinesKey pins the helper the `observer start` upgrade note uses
// to tell an install that inherited the old default (affected by D-307) from
// one that chose a value (untouched).
func TestFileDefinesKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		toml string // "" = no file
		want bool
	}{
		{name: "missing file", toml: "", want: false},
		{name: "no codeintel section", toml: "[observer]\nlog_level = \"info\"\n", want: false},
		{name: "partial section without enabled", toml: "[codeintel]\nretention_days = 7\n", want: false},
		{name: "explicit true", toml: "[codeintel]\nenabled = true\n", want: true},
		{name: "explicit false", toml: "[codeintel]\nenabled = false\n", want: true},
		{name: "dotted form", toml: "codeintel.enabled = true\n", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.toml")
			if tc.toml != "" {
				if err := os.WriteFile(path, []byte(tc.toml), 0o600); err != nil {
					t.Fatalf("write: %v", err)
				}
			}
			got, err := FileDefinesKey(path, "codeintel", "enabled")
			if err != nil {
				t.Fatalf("FileDefinesKey: %v", err)
			}
			if got != tc.want {
				t.Errorf("FileDefinesKey = %v, want %v", got, tc.want)
			}
		})
	}
	if _, err := FileDefinesKey(writeTemp(t, "[codeintel\n"), "codeintel", "enabled"); err == nil {
		t.Error("FileDefinesKey on a malformed file returned nil error, want a parse error")
	}
}

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}
