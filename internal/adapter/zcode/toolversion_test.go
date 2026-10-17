package zcode

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"testing"

	_ "modernc.org/sqlite"
)

// buildFixtureDB renders testdata/zcode/schema.sql then fixture.sql (the
// live-dumped DDL plus synthetic rows) into a db.sqlite under dir.
func buildFixtureDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".zcode", "cli", "db", "db.sqlite")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, name := range []string{"schema.sql", "fixture.sql"} {
		script, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "zcode", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(script)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	return path
}

// TestToolVersionEmittedFromSessionRow pins T-784: the session row's
// `version` column (the zcode build, "0.16.3" in the fixture) becomes one
// SessionToolVersion per touched session.
func TestToolVersionEmittedFromSessionRow(t *testing.T) {
	dbPath := buildFixtureDB(t)
	a := NewWithOptions(nil, []string{filepath.Dir(dbPath)})
	res, err := a.ParseSessionFile(context.Background(), dbPath, 0)
	if err != nil {
		t.Fatalf("ParseSessionFile: %v", err)
	}
	got := map[string]string{}
	for _, tv := range res.SessionToolVersions {
		if _, dup := got[tv.SessionID]; dup {
			t.Errorf("session %q stamped twice", tv.SessionID)
		}
		got[tv.SessionID] = tv.Version
	}
	var ids []string
	for id := range got {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		t.Fatal("no SessionToolVersions emitted")
	}
	for _, id := range ids {
		if got[id] != "0.16.3" {
			t.Errorf("session %q version = %q, want 0.16.3", id, got[id])
		}
	}
	if got["ses_a1b2c3d4e5f6"] == "" {
		t.Errorf("parent session not stamped; got %v", got)
	}

	// An incremental re-parse past the watermark touches nothing and
	// therefore stamps nothing.
	again, err := a.ParseSessionFile(context.Background(), dbPath, res.NewOffset)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if len(again.SessionToolVersions) != 0 {
		t.Errorf("re-parse at watermark emitted %+v, want none", again.SessionToolVersions)
	}
}

// TestToolVersionMissingColumnTolerated pins the older-schema posture: a
// session table without a `version` column yields no stamp and no error.
func TestToolVersionMissingColumnTolerated(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), ".zcode", "cli", "db", "db.sqlite")
	setupZcodeDB(t, dbPath)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE session DROP COLUMN version`); err != nil {
		t.Fatalf("drop column: %v", err)
	}
	db.Close()

	a := NewWithOptions(nil, []string{filepath.Dir(dbPath)})
	res, err := a.ParseSessionFile(context.Background(), dbPath, 0)
	if err != nil {
		t.Fatalf("ParseSessionFile on a version-less schema: %v", err)
	}
	if len(res.ToolEvents) == 0 {
		t.Fatal("expected tool events from the version-less schema")
	}
	if len(res.SessionToolVersions) != 0 {
		t.Errorf("SessionToolVersions = %+v, want none", res.SessionToolVersions)
	}
}
