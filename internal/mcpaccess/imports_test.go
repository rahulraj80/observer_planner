package mcpaccess

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins the package pure (CLAUDE.md rule 1, plan 3.3): no SQL, HTTP, fsnotify,
// and no store/orgserver/mcpgw/config package; every I/O lives at a seam.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{"database/sql", "net/http", "github.com/fsnotify/fsnotify", "github.com/marmutapp/superbased-observer/internal/store", "github.com/marmutapp/superbased-observer/internal/orgserver", "github.com/marmutapp/superbased-observer/internal/mcpgw", "github.com/marmutapp/superbased-observer/internal/config"},
		Reason:         "internal/mcpaccess is pure",
	})
}
