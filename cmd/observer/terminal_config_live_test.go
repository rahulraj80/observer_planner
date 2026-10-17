//go:build !sbo_omit_terminals

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// writeTerminalConfigT writes a config.toml with the given [terminal] body and
// moves its mtime forward by bump, so the holder's stamp check sees a change
// even on a filesystem with coarse timestamps.
func writeTerminalConfigT(t *testing.T, path, body string, bump time.Duration) {
	t.Helper()
	dbPath := filepath.Join(filepath.Dir(path), "observer.db")
	full := "[observer]\ndb_path = \"" + filepath.ToSlash(dbPath) + "\"\n" + body
	if err := os.WriteFile(path, []byte(full), 0o600); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(bump)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// TestTerminalConfigHolderReloadsOnSave pins T-489 (and the SSH half of
// T-488): the daemon's launch policy follows config.toml without a restart.
// A save that turns fresh launch on binds on the next read; a hand edit that
// breaks the file keeps the last good policy instead of tearing it down.
func TestTerminalConfigHolderReloadsOnSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeTerminalConfigT(t, path, "[terminal]\nenabled = true\n[terminal.launch]\nallow_fresh_agent = false\n", 0)
	boot, err := config.Load(config.LoadOptions{GlobalPath: path})
	if err != nil {
		t.Fatal(err)
	}
	h := newTerminalConfigHolder(boot.Terminal, path, nil)
	if h.Policy().AllowFresh {
		t.Fatal("boot policy: fresh launch should be off")
	}

	writeTerminalConfigT(t, path, "[terminal]\nenabled = true\n[terminal.launch]\nallow_fresh_agent = true\nallowed_tools = [\"claude-code\"]\nallow_shell = true\n", 2*time.Second)
	p := h.Policy()
	if !p.AllowFresh || !p.AllowShell || len(p.AllowedTools) != 1 || p.AllowedTools[0] != "claude-code" {
		t.Fatalf("policy after the save = %+v, want fresh+shell on with [claude-code]", p)
	}

	// A broken edit keeps the last good policy, and a later good edit is
	// still picked up (the stamp was not advanced past the broken one).
	h.load = func(string) (config.Config, error) { return config.Config{}, errors.New("broken edit") }
	writeTerminalConfigT(t, path, "[terminal]\nenabled = true\n[terminal.launch]\nallow_fresh_agent = false\n", 4*time.Second)
	if !h.Policy().AllowFresh {
		t.Fatal("a config that failed to load replaced the last good policy")
	}
	h.load = func(p string) (config.Config, error) { return config.Load(config.LoadOptions{GlobalPath: p}) }
	if h.Policy().AllowFresh {
		t.Fatal("the fixed config was not picked up after a failed reload")
	}
}

// TestTerminalConfigHolderNilAndPathless pins the fail-closed edges: a nil
// holder denies everything, and a holder with no config path stays on its
// boot snapshot.
func TestTerminalConfigHolderNilAndPathless(t *testing.T) {
	var nilHolder *terminalConfigHolder
	if p := nilHolder.Policy(); p.AllowFresh || p.AllowShell || p.AllowSSH {
		t.Fatalf("nil holder policy = %+v, want deny-all", p)
	}
	var tc config.TerminalConfig
	tc.Enabled, tc.Launch.AllowFreshAgent = true, true
	h := newTerminalConfigHolder(tc, "", nil)
	if !h.Policy().AllowFresh {
		t.Fatal("pathless holder lost its boot snapshot")
	}
}
