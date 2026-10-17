package taskreport

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/taskreport as a pure cost/report
// builder (CLAUDE.md rule 1): SQL lives in internal/store, HTTP in the
// dashboard. internal/intelligence/cost is DELIBERATELY allowed (doc.go).
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "internal/taskreport must stay a pure builder over internal/store's I/O",
	})
}
