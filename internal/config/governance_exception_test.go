package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/govern/sidecar"
)

func govWriteExceptions(t *testing.T, dbDir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dbDir, sidecar.ExceptionsFilename), []byte(body), 0o600); err != nil {
		t.Fatalf("write exceptions: %v", err)
	}
}

// TestFeatureGrantExceptionLiftsOnePin (request-grants G8): a pinned setting
// the daemon recorded a feature-access exception for keeps the node's OWN
// value while the exception lasts, every other pin still applies, and the
// reader ends the exception on its own at its until instant, whether or not
// the daemon is alive to rewrite the file. The exception only ever REMOVES a
// pin: it never sets a value. Exceptions live in their own file, bound to
// the sidecar's effective hash.
func TestFeatureGrantExceptionLiftsOnePin(t *testing.T) {
	dbDir := t.TempDir()
	cfgPath := govTempConfig(t, dbDir, "[guard]\nenabled = false\nmode = \"off\"\n")
	until := time.Date(2026, 10, 4, 18, 0, 0, 0, time.UTC)
	govWriteSidecar(t, dbDir, `{"schema":1,"state":"applied","effective_hash":"h1","pinned":{"guard.enabled":true,"guard.mode":"enforce"}}`)
	govWriteExceptions(t, dbDir, `{"schema":1,"effective_hash":"h1","exceptions":{"guard.mode":{"grant_id":31,"until":"2026-10-04T18:00:00Z"},"routing.enabled":{"grant_id":32,"until":"2026-10-04T18:00:00Z"}}}`)

	at := func(now time.Time) (Config, GovernanceOutcome) {
		t.Helper()
		cfg, out, err := LoadGovernance(LoadOptions{GlobalPath: cfgPath, Env: noEnv, GovernanceNow: func() time.Time { return now }})
		if err != nil {
			t.Fatalf("LoadGovernance: %v", err)
		}
		return cfg, out
	}

	cfg, out := at(until.Add(-time.Hour))
	if !cfg.Guard.Enabled || cfg.Guard.Mode != "off" {
		t.Fatalf("while excepted: enabled=%v mode=%q, want the pin on enabled and the node's own mode", cfg.Guard.Enabled, cfg.Guard.Mode)
	}
	if out.Excepted["guard.mode"] != 31 || len(out.Excepted) != 1 {
		t.Fatalf("excepted = %v, want only guard.mode (an exception for an unpinned key sets nothing)", out.Excepted)
	}
	if len(out.Applied) != 1 || out.Applied[0] != "guard.enabled" {
		t.Fatalf("applied = %v", out.Applied)
	}
	if cfg.Routing.Enabled {
		t.Fatal("an exception for an unpinned key changed its value")
	}

	cfg, out = at(until)
	if cfg.Guard.Mode != "enforce" || len(out.Excepted) != 0 {
		t.Fatalf("at the until instant the pin is back: mode=%q excepted=%v", cfg.Guard.Mode, out.Excepted)
	}
}

// TestFeatureGrantExceptionKeepsThePin: every way the exceptions file can
// fail to apply leaves the pin in force (the stricter reading).
func TestFeatureGrantExceptionKeepsThePin(t *testing.T) {
	cases := []struct{ name, sidecar, exceptions string }{
		{
			name:       "malformed until",
			sidecar:    `{"schema":1,"state":"applied","effective_hash":"h1","pinned":{"guard.mode":"enforce"}}`,
			exceptions: `{"schema":1,"effective_hash":"h1","exceptions":{"guard.mode":{"grant_id":31,"until":"soon"}}}`,
		},
		{
			name:       "bound to an earlier posture",
			sidecar:    `{"schema":1,"state":"applied","effective_hash":"h2","pinned":{"guard.mode":"enforce"}}`,
			exceptions: `{"schema":1,"effective_hash":"h1","exceptions":{"guard.mode":{"grant_id":31,"until":"2999-01-01T00:00:00Z"}}}`,
		},
		{
			name:       "malformed file",
			sidecar:    `{"schema":1,"state":"applied","effective_hash":"h1","pinned":{"guard.mode":"enforce"}}`,
			exceptions: `{"schema":1,`,
		},
		{
			name:    "no exceptions file",
			sidecar: `{"schema":1,"state":"applied","effective_hash":"h1","pinned":{"guard.mode":"enforce"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dbDir := t.TempDir()
			cfgPath := govTempConfig(t, dbDir, "[guard]\nmode = \"off\"\n")
			govWriteSidecar(t, dbDir, tc.sidecar)
			if tc.exceptions != "" {
				govWriteExceptions(t, dbDir, tc.exceptions)
			}
			cfg, out, err := LoadGovernance(LoadOptions{GlobalPath: cfgPath, Env: noEnv})
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Guard.Mode != "enforce" || len(out.Excepted) != 0 {
				t.Fatalf("mode=%q excepted=%v, want the pin and no exception", cfg.Guard.Mode, out.Excepted)
			}
		})
	}
}
