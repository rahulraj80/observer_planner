//go:build linux && !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/sandboxremote"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// The stored class of a SandHold run (demo round 2026-11-12): the run was
// recorded with the row's floor (process) and the launch's tier, and the
// database kept them after the node answered, so analytics, Security and
// exports read process for a box the node made Shielded. Once the node
// answers, the stored run takes the class and tier its words map to.

// boxClassTestStore opens a throwaway node database with one SandHold run
// and one local run, both recorded the way a launch records them.
func boxClassTestStore(t *testing.T) (*store.Store, *sql.DB) {
	t.Helper()
	database, err := db.Open(context.Background(), db.Options{Path: filepath.Join(t.TempDir(), "observer.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	st := store.New(database)
	for id, stack := range map[string]string{"term-run-1": sandboxremote.VendorSandHold, "term-run-2": "bwrap", "term-run-3": sandboxremote.VendorE2B} {
		iso := store.TerminalRunIsolation{Class: "process", Stack: []string{stack}, EgressTier: "internet", CaptureLevel: "full", CaptureOrigin: "remote_sandbox"}
		if err := st.InsertTerminalRun(context.Background(), store.TerminalRun{RunID: id, Tool: "claude-code", Kind: "fresh", LaunchedAt: time.Now().UTC(), Isolation: iso}); err != nil {
			t.Fatal(err)
		}
	}
	return st, database
}

func boxClassOf(t *testing.T, database *sql.DB, id string) (class, egress string) {
	t.Helper()
	if err := database.QueryRow(`SELECT isolation_class, egress_tier FROM terminal_run WHERE run_id = ?`, id).Scan(&class, &egress); err != nil {
		t.Fatalf("load %s: %v", id, err)
	}
	return class, egress
}

// TestStoreHostedBoxNoteRecordsTheNodesClass: each tier word maps to its
// class and each network word to its tier on the SandHold run; a local run
// and a one-isolation hosted run keep what they were launched with.
func TestStoreHostedBoxNoteRecordsTheNodesClass(t *testing.T) {
	st, database := boxClassTestStore(t)
	ctx := context.Background()
	for _, tc := range []struct{ tier, network, class, egress string }{
		{"shielded", "limited", "container", "proxy_only"},
		{"sealed", "none", "userspace_kernel", "none"},
		{"vault", "open", "hw_vm", "internet"},
		{"shared", "", "process", "internet"},
	} {
		if _, err := storeHostedBoxNote(ctx, st, sandboxremote.VendorSandHold, "term-run-1", hostedBoxNote{V: 1, Isolation: tc.tier, Network: tc.network}); err != nil {
			t.Fatal(err)
		}
		if c, e := boxClassOf(t, database, "term-run-1"); c != tc.class || e != tc.egress {
			t.Fatalf("%s/%s stored %s / %s, want %s / %s", tc.tier, tc.network, c, e, tc.class, tc.egress)
		}
	}
	note := hostedBoxNote{V: 1, Isolation: "vault", Network: "none"}
	for _, tc := range []struct{ backend, id string }{
		{"bwrap", "term-run-2"},
		{sandboxremote.VendorE2B, "term-run-3"},
		{sandboxremote.VendorSandHold, "term-run-2"},
	} {
		if changed, err := storeHostedBoxNote(ctx, st, tc.backend, tc.id, note); err != nil || changed {
			t.Fatalf("%s on %s: changed=%v err=%v", tc.backend, tc.id, changed, err)
		}
		if c, e := boxClassOf(t, database, tc.id); c != "process" || e != "internet" {
			t.Fatalf("%s was rewritten to %s / %s", tc.id, c, e)
		}
	}
}

// TestBackfillHostedBoxClasses: a daemon start records the class of every
// kept note on its stored run (runs recorded before the helper wrote it),
// once.
func TestBackfillHostedBoxClasses(t *testing.T) {
	st, database := boxClassTestStore(t)
	obs := t.TempDir()
	ctx := context.Background()
	for id, n := range map[string]hostedBoxNote{
		"term-run-1": {V: 1, Isolation: "shielded", Network: "limited", RunsOn: "runc"},
		"term-run-2": {V: 1, Isolation: "vault", Network: "none"},
		"term-run-9": {V: 1, Isolation: "sealed"},
	} {
		if err := hostedWriteOwned(hostedBoxNotePath(obs, id), n); err != nil {
			t.Fatal(err)
		}
	}
	n, err := backfillHostedBoxClasses(ctx, st, obs)
	if err != nil || n != 1 {
		t.Fatalf("backfill = %d, %v; want the one SandHold run", n, err)
	}
	if c, e := boxClassOf(t, database, "term-run-1"); c != "container" || e != "proxy_only" {
		t.Fatalf("SandHold run stored %s / %s", c, e)
	}
	if c, _ := boxClassOf(t, database, "term-run-2"); c != "process" {
		t.Fatalf("a note rewrote a local run to %s", c)
	}
	if n, err := backfillHostedBoxClasses(ctx, st, obs); err != nil || n != 0 {
		t.Fatalf("second backfill = %d, %v; want nothing", n, err)
	}
	if n, err := backfillHostedBoxClasses(ctx, st, t.TempDir()); err != nil || n != 0 {
		t.Fatalf("no notes = %d, %v", n, err)
	}
}

// TestHostedHelperRecordsTheBoxClass: the helper hands the node's answer,
// under the stored run's id, to the database write as soon as the create
// is answered.
func TestHostedHelperRecordsTheBoxClass(t *testing.T) {
	hh := newHostedHarness(t, sandboxremote.VendorE2B)
	hh.client.extra = map[string]string{"isolation": "sealed", "network": "none", "runs_on": "gvisor"}
	var mu sync.Mutex
	var got []string
	hh.run.recordBox = func(_ context.Context, backend, id string, n hostedBoxNote) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, backend+" "+id+" "+n.Isolation+" "+n.Network)
	}
	code := make(chan int, 1)
	go func() { code <- hh.run.drive(context.Background()) }()
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(got) > 0 })
	hostedEndRun(t, hh, code)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != sandboxremote.VendorE2B+" run-h1 sealed none" {
		t.Fatalf("recorded %q", got)
	}
}
