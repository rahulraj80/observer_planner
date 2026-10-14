package provenance

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins provenance as a pure DATA leaf (CLAUDE.md
// Module Boundaries #1/#5).
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "pure DATA leaf boundary",
	})
}
