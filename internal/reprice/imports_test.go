package reprice

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins the module boundary (CLAUDE.md "core logic lives
// in a pure package"): the planner reads no database, speaks no HTTP, watches
// no files and never imports the cost engine; the price is injected.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"os/exec",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/intelligence/cost",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/integration",
		},
		Reason: "the re-price planner is pure",
	})
}
