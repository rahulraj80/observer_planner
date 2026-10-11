package db

import (
	"context"
	"path/filepath"
	"testing"
)

// TestOpenDefersMigrationsWhenTheDeferralSaysSo is U3's red/green at the
// db seam: with a deferral installed that answers true, Open leaves a
// schema that is behind exactly where it was, and the deferral sees the
// applied and latest versions. A current schema never consults it.
func TestOpenDefersMigrationsWhenTheDeferralSaysSo(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "observer.db")
	d, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	latest := LatestSchemaVersion()
	if v, _ := Version(ctx, d); v != latest {
		t.Fatalf("fresh Open: version %d, want %d", v, latest)
	}
	// Pretend the file is one release behind (the daemon that owns it has
	// not migrated yet).
	if _, err := d.ExecContext(ctx, `UPDATE schema_meta SET value = ? WHERE key = 'version'`, latest-1); err != nil {
		t.Fatal(err)
	}
	_ = d.Close()

	var gotApplied, gotLatest, calls int
	SetMigrationDeferral(func(_ string, applied, l int) bool {
		calls++
		gotApplied, gotLatest = applied, l
		return true
	})
	t.Cleanup(func() { SetMigrationDeferral(nil) })
	d2, err := Open(ctx, Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	if v, _ := Version(ctx, d2); v != latest-1 {
		t.Fatalf("deferred Open migrated: version %d, want %d", v, latest-1)
	}
	if calls != 1 || gotApplied != latest-1 || gotLatest != latest {
		t.Fatalf("deferral saw (%d, %d) in %d calls, want (%d, %d) once", gotApplied, gotLatest, calls, latest-1, latest)
	}

	// A current schema never asks.
	path2 := filepath.Join(t.TempDir(), "current.db")
	SetMigrationDeferral(nil)
	d3, err := Open(ctx, Options{Path: path2})
	if err != nil {
		t.Fatal(err)
	}
	_ = d3.Close()
	calls = 0
	SetMigrationDeferral(func(string, int, int) bool { calls++; return true })
	d4, err := Open(ctx, Options{Path: path2})
	if err != nil {
		t.Fatal(err)
	}
	_ = d4.Close()
	if calls != 0 {
		t.Fatal("a current schema consulted the deferral")
	}
}
