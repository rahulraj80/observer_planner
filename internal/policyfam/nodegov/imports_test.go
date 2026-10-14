package nodegov

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins the node.governance family COMPILER pure
// (CLAUDE.md #1); internal/govern depends on THIS package, never the
// reverse.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/config",
			"github.com/marmutapp/superbased-observer/internal/obs",
			"github.com/marmutapp/superbased-observer/internal/proxy",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/govern",
		},
		Reason: "internal/policyfam/nodegov must stay pure",
	})
}
