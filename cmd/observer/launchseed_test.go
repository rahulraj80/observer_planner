package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/pidbridge"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// newLaunchSeedTestDB opens a throwaway observer DB (all migrations applied)
// and returns its path plus the store/bridge pair the sweep consumes.
func newLaunchSeedTestDB(t *testing.T) (path string, database *sql.DB, st *store.Store, bridge *pidbridge.Store) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "launchseed.db")
	database, err := dbtemplate.Open(context.Background(), db.Options{Path: path})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return path, database, store.New(database), pidbridge.New(database)
}

func TestRecordLaunchSeed_PersistsPastChildExit(t *testing.T) {
	// The launcher is fire-and-forget by design (live-verified 2026-08-21:
	// an exit-retract deleted headless-launch seeds before the 90s sweep
	// could consume them). After "exit" — i.e. simply returning from
	// recordLaunchSeed — the seed MUST still be pending so the sweep can
	// match it against the session the watcher ingests moments later.
	path, _, _, _ := newLaunchSeedTestDB(t)

	recordLaunchSeed(path, "opencode", "/proj", 555, nil)

	database, err := dbtemplate.Open(context.Background(), db.Options{Path: path})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer database.Close()
	pending, err := store.New(database).PendingLaunchSeeds(context.Background(), time.Hour)
	if err != nil || len(pending) != 1 || pending[0].PID != 555 {
		t.Fatalf("pending = (%+v, %v), want one seed for pid 555 still pending after exit", pending, err)
	}
}

func TestRecordLaunchSeed_DisabledOnEmptyPathOrPID(t *testing.T) {
	recordLaunchSeed("", "opencode", "/proj", 1, nil) // must not panic / no-op
	path, _, _, _ := newLaunchSeedTestDB(t)
	recordLaunchSeed(path, "opencode", "/proj", 0, nil)
	database, err := dbtemplate.Open(context.Background(), db.Options{Path: path})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer database.Close()
	pending, err := store.New(database).PendingLaunchSeeds(context.Background(), time.Hour)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending = (%+v, %v), want zero pid to write nothing", pending, err)
	}
}
