package verbosity

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/verbosity pure (CLAUDE.md rule 1):
// I/O is injected at the store seam and the ingest graft, and it owns its
// language table (plan section 4) instead of reaching into codeintel.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/codeintel",
			"github.com/marmutapp/superbased-observer/internal/codeintel/parse",
		},
		Reason: "internal/verbosity must stay pure",
	})
}
