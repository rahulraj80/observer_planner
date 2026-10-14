package api

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestBoundaryImports pins api as the HTTP owner that owns no SQL (plan 7 item
// 7): database access goes through internal/cloudserver/store.
func TestBoundaryImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"github.com/jackc/pgx/v5",
			"github.com/jackc/pgx/v5/pgxpool",
		},
		Reason: "SQL and pgx belong in internal/cloudserver/store, not api",
	})
}
