package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
)

// TestGuardBudgetLineNeverEnrolledSaysNotEnrolled is U11: `observer guard
// status` on a node that was never enrolled reported fetch_state=unreachable
// (an outage word for a node with nothing to fetch). It now says the node is
// not enrolled.
func TestGuardBudgetLineNeverEnrolledSaysNotEnrolled(t *testing.T) {
	ctx := context.Background()
	database, err := dbtemplate.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "observer.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	cfg := config.Default()
	cfg.Guard.Budget.FromOrg = true
	line := guardBudgetPostureLineWithGrant(ctx, cfg, database, false)
	if !strings.Contains(line, "fetch_state=not_enrolled") || !strings.Contains(line, "not enrolled in an organization") {
		t.Fatalf("line = %q, want the not-enrolled wording", line)
	}
	if strings.Contains(line, "fetch_state=unreachable") {
		t.Fatalf("line = %q still reports an outage", line)
	}
}
