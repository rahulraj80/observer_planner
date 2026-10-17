package modelvalue

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins modelvalue PURE aggregation: SQL rows are
// injected by internal/store/modelvalue.go.
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/proxy",
			"github.com/marmutapp/superbased-observer/internal/db",
			"github.com/marmutapp/superbased-observer/internal/adapter",
			"github.com/marmutapp/superbased-observer/internal/watcher",
			"github.com/marmutapp/superbased-observer/internal/hook",
			"github.com/marmutapp/superbased-observer/internal/mcp",
			"github.com/marmutapp/superbased-observer/internal/config",
			"github.com/marmutapp/superbased-observer/internal/intelligence/advisor",
			"github.com/marmutapp/superbased-observer/internal/intelligence/dashboard",
		},
		Reason: "pure-aggregation boundary",
	})
}
