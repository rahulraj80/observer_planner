package localpdp

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins the package pure (CLAUDE.md rule 1) and the reverse-import boundary (doc3
// 11.7 W4d: the node seam consults plain result types, never internal/mcpgw).
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{"database/sql", "net/http", "github.com/fsnotify/fsnotify", "github.com/marmutapp/superbased-observer/internal/store", "github.com/marmutapp/superbased-observer/internal/orgserver", "github.com/marmutapp/superbased-observer/internal/mcpgw", "github.com/marmutapp/superbased-observer/internal/orgclient", "github.com/marmutapp/superbased-observer/internal/config", "github.com/marmutapp/superbased-observer/internal/proxy", "github.com/marmutapp/superbased-observer/internal/guard"},
		Reason:         "internal/mcprelay/localpdp is pure",
	})
}
