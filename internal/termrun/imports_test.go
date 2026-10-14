package termrun

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/termrun pure (CLAUDE.md rule 1): I/O
// is injected at the store seam (internal/store/termrun.go) and the terminal
// application service.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "internal/termrun must stay pure",
	})
}
