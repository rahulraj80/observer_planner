package govern

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/govern pure (CLAUDE.md #1): every I/O
// dependency is injected by cmd/observer/nodegov_wire.go.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/config",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/obs",
			"github.com/marmutapp/superbased-observer/internal/proxy",
			"github.com/marmutapp/superbased-observer/internal/orgclient",
		},
		Reason: "internal/govern must stay pure",
	})
}
