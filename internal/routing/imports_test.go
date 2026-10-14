package routing

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins the routing package pure (CLAUDE.md Module
// Boundaries, spec section 24.1, model-routing spec section R5): pricing
// arrives via PriceFn, evidence rows via caller-built structs.
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
			"github.com/marmutapp/superbased-observer/internal/intelligence",
			"github.com/marmutapp/superbased-observer/internal/mcp",
			"github.com/marmutapp/superbased-observer/internal/config",
			"github.com/marmutapp/superbased-observer/internal/freshness",
			"github.com/marmutapp/superbased-observer/internal/scoring",
			"github.com/marmutapp/superbased-observer/internal/compression",
		},
		Reason: "module boundary section 24.1 / section R5",
	})
}
