package kilocode

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// TestCLIToolVersionEmitted pins Issue 2 (migration 125) for the Kilo CLI:
// the session row's `version` column ("7.3.40", the live-captured build)
// becomes exactly one SessionToolVersion for the touched session, and a
// session with no new rows in the slice is not stamped.
func TestCLIToolVersionEmitted(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "kilo.db")
	setupKiloDB(t, path)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`ALTER TABLE session ADD COLUMN version TEXT NOT NULL DEFAULT ''`,
		`UPDATE session SET version = '7.3.40' WHERE id = 'ses_1'`,
		// A second session with a version but no message/part rows: it is
		// never touched by this slice, so it must not be stamped.
		`INSERT INTO session(id, project_id, directory, time_updated, version) VALUES ('ses_idle', 'proj_a', '/tmp/kilo-test', 500, '7.3.39')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
	db.Close()

	a := NewCLIWithOptions(nil, []string{root})
	res, err := a.ParseSessionFile(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("ParseSessionFile: %v", err)
	}
	if len(res.SessionToolVersions) != 1 {
		t.Fatalf("SessionToolVersions = %+v, want exactly one", res.SessionToolVersions)
	}
	if got := res.SessionToolVersions[0]; got.SessionID != "ses_1" || got.Version != "7.3.40" {
		t.Errorf("stamp = %+v, want {ses_1 7.3.40}", got)
	}
}

// TestCLIToolVersionMissingColumnTolerated pins that an older Kilo schema
// with no session.version column parses cleanly and stamps nothing.
func TestCLIToolVersionMissingColumnTolerated(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "kilo.db")
	setupKiloDB(t, path) // no version column

	a := NewCLIWithOptions(nil, []string{root})
	res, err := a.ParseSessionFile(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("ParseSessionFile: %v", err)
	}
	if len(res.ToolEvents) == 0 {
		t.Fatal("expected tool events")
	}
	if len(res.SessionToolVersions) != 0 {
		t.Errorf("SessionToolVersions = %+v, want none", res.SessionToolVersions)
	}
}
