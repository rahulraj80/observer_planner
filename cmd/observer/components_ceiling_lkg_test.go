package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/managedconfig"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestCeilingLastKnownPersistsAndHolds pins T-682 (lane MP-REL): a valid
// local ceiling is stored as the node's last-known set (agent migration
// 160), and a later invalid source keeps that set in force instead of
// falling to core only. Before the last-known set was persisted and fed
// back, every caller passed nil, so the second step resolved
// invalid_core_only.
func TestCeilingLastKnownPersistsAndHolds(t *testing.T) {
	ctx := context.Background()
	database, err := dbtemplate.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "observer.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	st := store.New(database)

	valid := managedconfig.Probe{
		Found: true, Source: "/etc/superbased/managed.toml", Format: "toml",
		Data: []byte("[ceiling]\ncatalog_version = 1\ncomponents = [\"capture.proxy\", \"codeintel\"]\n"),
	}
	broken := managedconfig.Probe{Found: true, Source: "/etc/superbased/managed.toml", FailedCheck: "owner_not_root"}
	loadWith := func(p managedconfig.Probe) func(*managedconfig.Ceiling) managedconfig.Result {
		return func(lk *managedconfig.Ceiling) managedconfig.Result { return managedconfig.Decide(p, lk) }
	}

	// No database: an invalid source is core only (no last-known set).
	if r := loadCeilingWithLastKnown(ctx, loadWith(broken), nil); r.Status != managedconfig.StatusInvalidCoreOnly {
		t.Fatalf("no store, invalid source: %s, want %s", r.Status, managedconfig.StatusInvalidCoreOnly)
	}

	r := loadCeilingWithLastKnown(ctx, loadWith(valid), st)
	if r.Status != managedconfig.StatusValid || !r.Persist {
		t.Fatalf("valid source: status %s persist %v", r.Status, r.Persist)
	}
	js, err := st.LoadCeilingLastKnown(ctx)
	if err != nil || !strings.Contains(js, `"capture.proxy"`) || !strings.Contains(js, `"source_hash"`) {
		t.Fatalf("stored last-known = %q, %v", js, err)
	}
	// Same source again: nothing new to persist.
	if r := loadCeilingWithLastKnown(ctx, loadWith(valid), st); r.Persist {
		t.Error("an unchanged valid source asked to persist again")
	}

	r = loadCeilingWithLastKnown(ctx, loadWith(broken), st)
	if r.Status != managedconfig.StatusInvalidLastKnown {
		t.Fatalf("invalid source after a valid one: %s, want %s", r.Status, managedconfig.StatusInvalidLastKnown)
	}
	if pinned, ids := r.Pinned(); !pinned || strings.Join(ids, ",") != "capture.proxy,codeintel" {
		t.Fatalf("ceiling in force = %v %v, want the last valid set", pinned, ids)
	}
	if r.UpdatesAllowed {
		t.Error("an invalid ceiling source must refuse updates")
	}
	// The invalid read never overwrote the stored set.
	if js2, _ := st.LoadCeilingLastKnown(ctx); js2 != js {
		t.Errorf("last-known changed on an invalid read: %q -> %q", js, js2)
	}
}

// TestCeilingLastKnownToleratesOldDatabase: a short-lived process opens the
// database without migrating it; before migration 160 there is no column,
// which reads as no last-known set and never errors.
func TestCeilingLastKnownToleratesOldDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "observer.db")
	database, err := dbtemplate.Open(ctx, db.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `ALTER TABLE update_state DROP COLUMN ceiling_last_known`); err != nil {
		t.Fatal(err)
	}
	st := store.New(database)
	defer database.Close()
	if js, err := st.LoadCeilingLastKnown(ctx); err != nil || js != "" {
		t.Fatalf("pre-160 database: %q, %v; want \"\" and no error", js, err)
	}
	broken := managedconfig.Probe{Found: true, Source: "x", FailedCheck: "mode"}
	if r := loadCeilingWithLastKnown(ctx, func(lk *managedconfig.Ceiling) managedconfig.Result { return managedconfig.Decide(broken, lk) }, st); r.Status != managedconfig.StatusInvalidCoreOnly {
		t.Fatalf("pre-160 database, invalid source: %s", r.Status)
	}
}
