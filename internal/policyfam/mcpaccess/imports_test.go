package mcpaccess

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins the family package pure (CLAUDE.md section 1):
// no SQL, HTTP, fsnotify, config, store, orgserver or mcpgw.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/config",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/orgserver",
			"github.com/marmutapp/superbased-observer/internal/mcpgw",
		},
		Reason: "internal/policyfam/mcpaccess must stay pure",
	})
}
