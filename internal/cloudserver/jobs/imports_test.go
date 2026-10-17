package jobs

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestBoundaryImports pins that jobs (queue + worker) owns neither HTTP nor SQL:
// it composes the store seam and the queue interface (plan 7 item 7).
func TestBoundaryImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"net/http",
			"database/sql",
			"github.com/jackc/pgx/v5",
		},
		Reason: "jobs owns no HTTP; SQL belongs in internal/cloudserver/store",
	})
}
