package opencode

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// TestToolVersionEmitted pins T-784 for OpenCode: the session row's
// `version` column (grounded live 2026-10-13 as `version text NOT NULL`,
// value "1.14.29") becomes one SessionToolVersion keyed to the session id
// the rows use; an untouched session is not stamped.
func TestToolVersionEmitted(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "opencode.db")
	setupOpenCodeDB(t, path)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`ALTER TABLE session ADD COLUMN version TEXT NOT NULL DEFAULT ''`,
		`UPDATE session SET version = '1.14.29' WHERE id = 'ses_1'`,
		`INSERT INTO session(id, directory, time_updated, version) VALUES ('ses_idle', '/tmp/idle', 100, '1.14.28')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
	db.Close()

	res, err := NewWithOptions(nil, []string{root}).ParseSessionFile(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("ParseSessionFile: %v", err)
	}
	if len(res.SessionToolVersions) != 1 {
		t.Fatalf("SessionToolVersions = %+v, want exactly one", res.SessionToolVersions)
	}
	got := res.SessionToolVersions[0]
	if got.SessionID != "ses_1" || got.Version != "1.14.29" {
		t.Errorf("stamp = %+v, want {ses_1 1.14.29}", got)
	}
	if len(res.ToolEvents) == 0 || res.ToolEvents[0].SessionID != got.SessionID {
		t.Errorf("stamp session does not match the rows' session")
	}
}

// TestToolVersionMissingColumnTolerated pins that a session table without
// a version column parses cleanly and stamps nothing.
func TestToolVersionMissingColumnTolerated(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "opencode.db")
	setupOpenCodeDB(t, path) // no version column
	res, err := NewWithOptions(nil, []string{root}).ParseSessionFile(context.Background(), path, 0)
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
