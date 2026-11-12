//go:build linux && !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/sandboxrelayd"
	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
)

// TestHostedBoxNoteRecordsWhatTheNodeMade (demo screenshots 2026-11-12): a
// template asked a SandHold node for Shielded with a Limited network and
// got exactly that, while Observer said process / internet everywhere. The
// helper leaves the node's answer as the run's box note, and the daemon's
// box words read it back in the node's words, never the row's floor.
func TestHostedBoxNoteRecordsWhatTheNodeMade(t *testing.T) {
	hh := newHostedHarness(t, sandboxremote.VendorE2B)
	hh.client.extra = map[string]string{"isolation": "shielded", "network": "limited", "runs_on": "runc"}
	code := make(chan int, 1)
	go func() { code <- hh.run.drive(context.Background()) }()
	obs := hh.run.plan.ObserverDir
	waitFor(t, func() bool { _, ok := readHostedBoxNote(obs, "run-h1"); return ok })
	if c := hostedEndRun(t, hh, code); c != 5 {
		t.Errorf("exit %d", c)
	}
	// The note outlives the run (the history reads it after the end).
	got, ok := runBoxWords(obs, "run-h1", sandboxremote.VendorSandHold, false)
	want := dashapi.BoxWords{Isolation: "shielded", Network: "limited", RunsOn: "runc"}
	if !ok || got != want {
		t.Fatalf("box words = %+v, %v; want %+v", got, ok, want)
	}
	fi, err := os.Stat(hostedBoxNotePath(obs, "run-h1"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("note %v, %v", fi, err)
	}
}

// TestHostedBoxWordsRows: the words source answers only for a row whose node
// decides the isolation; until the node says, a live run is pending (never
// the floor), an ended one is unknown; a note in anything but the node's
// words is not kept.
func TestHostedBoxWordsRows(t *testing.T) {
	obs := t.TempDir()
	if _, ok := runBoxWords(obs, "run-a", "bwrap", true); ok {
		t.Fatal("a local row reports box words")
	}
	if _, ok := runBoxWords(obs, "run-a", sandboxremote.VendorE2B, true); ok {
		t.Fatal("a one-isolation hosted row reports box words")
	}
	if w, ok := runBoxWords(obs, "run-a", sandboxremote.VendorSandHold, true); !ok || !w.Pending || w.Known() {
		t.Fatalf("live, no answer yet = %+v, %v", w, ok)
	}
	if w, ok := runBoxWords(obs, "run-a", sandboxremote.VendorSandHold, false); !ok || w.Pending || w.Known() {
		t.Fatalf("ended, no answer = %+v, %v", w, ok)
	}
	writeHostedBoxNote(obs, "run-a", map[string]string{"isolation": "process", "network": "internet"})
	if _, ok := readHostedBoxNote(obs, "run-a"); ok {
		t.Fatal("Observer's own words were kept as the node's")
	}
	writeHostedBoxNote(obs, "run-a", map[string]string{"isolation": "Vault", "network": "everything", "runs_on": "kata; rm -rf"})
	n, ok := readHostedBoxNote(obs, "run-a")
	if !ok || n.Isolation != "vault" || n.Network != "" || n.RunsOn != "" {
		t.Fatalf("note = %+v, %v", n, ok)
	}
}

// TestHostedBoxNoteKeyedByTerminalRun: the note is written under the
// terminal run the relay run is bound to, so the run history (which knows
// only terminal run ids) finds it; old notes are swept.
func TestHostedBoxNoteKeyedByTerminalRun(t *testing.T) {
	obs := t.TempDir()
	rec, err := sandboxrelayd.NewRunRecord(sandboxrelayd.RunRecord{RunID: "sbx-1", Tool: "claude-code", ProjectRoot: obs, WorkspaceDir: obs, Roots: []string{obs}})
	if err != nil {
		t.Fatal(err)
	}
	if err := sandboxrelayd.WriteRun(obs, rec); err != nil {
		t.Fatal(err)
	}
	if err := sandboxrelayd.BindTerminalRun(obs, "sbx-1", "term-run-1"); err != nil {
		t.Fatal(err)
	}
	stale := hostedBoxNotePath(obs, "old-run")
	if err := hostedWriteOwned(stale, hostedBoxNote{V: 1, Isolation: "shared"}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-hostedBoxNoteKeep - time.Hour)
	_ = os.Chtimes(stale, old, old)
	writeHostedBoxNote(obs, "sbx-1", map[string]string{"isolation": "sealed", "network": "none", "runs_on": "gvisor"})
	if n, ok := readHostedBoxNote(obs, "term-run-1"); !ok || n.Isolation != "sealed" || n.RunsOn != "gvisor" {
		t.Fatalf("note under the terminal run = %+v, %v", n, ok)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("a stale note was kept: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(stale))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "sbx-1") {
			t.Fatalf("note also kept under the sandbox run id: %s", e.Name())
		}
	}
}
