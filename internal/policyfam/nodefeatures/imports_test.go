package nodefeatures

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins the node.features family COMPILER pure
// (CLAUDE.md section 1): reused by orgserver, orgclient and cmd/observer
// (mirrors the other internal/policyfam packages).
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/config",
			"github.com/marmutapp/superbased-observer/internal/obs",
			"github.com/marmutapp/superbased-observer/internal/proxy",
		},
		Reason: "internal/policyfam/nodefeatures must stay pure",
	})
}
