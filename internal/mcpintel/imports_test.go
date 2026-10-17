package mcpintel

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins the P11 purity rule (doc3 11.12b, R10.12) for internal/mcpintel and every
// subpackage beneath it (walked recursively, so a new subpackage is covered with no edit here).
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Recursive:      true,
		ForbiddenTrees: []string{"database/sql", "net/http", "github.com/fsnotify/fsnotify", "github.com/marmutapp/superbased-observer/internal/store", "github.com/marmutapp/superbased-observer/internal/db", "github.com/marmutapp/superbased-observer/internal/orgserver", "github.com/marmutapp/superbased-observer/internal/intelligence/discover"},
		Reason:         "internal/mcpintel/** must stay pure (doc3 11.12b)",
	})
}
