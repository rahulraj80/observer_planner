package routeattest

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins routeattest pure, recursively (CLAUDE.md
// Module Boundaries rule 1, design section 3.3): I/O is injected via
// TrafficSource and the config-lane file reads live in internal/proxyroute.
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		Recursive: true,
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/db",
			"github.com/marmutapp/superbased-observer/internal/proxy",
			"github.com/marmutapp/superbased-observer/internal/watcher",
			"github.com/marmutapp/superbased-observer/internal/adapter",
		},
		Reason: "internal/routeattest must stay pure, see doc.go",
	})
}
