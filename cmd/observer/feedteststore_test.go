package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// feedTestStore opens a fresh migrated node database for a test. It lives
// untagged (moved from pricing_test.go, which carries
// !sbo_omit_egress_pricing_feed) because org pricing tests that run in every
// build use it too (REVIEW-4 P4-08).
func feedTestStore(t *testing.T) (*store.Store, *sql.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "observer.db")
	database, err := dbtemplate.Open(context.Background(), db.Options{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return store.New(database), database
}
