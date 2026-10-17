package providers

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins the gateway.providers family COMPILER pure
// (CLAUDE.md section 1): reused by orgserver and orgclient, the
// reverse-import boundary this package exists to enable.
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
		Reason: "internal/policyfam/providers must stay pure",
	})
}
