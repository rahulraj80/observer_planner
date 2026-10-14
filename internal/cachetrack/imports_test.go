package cachetrack

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins cachetrack pure logic (spec 24.1): no
// infrastructure imports and no other observer subsystem.
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
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
			"github.com/marmutapp/superbased-observer/internal/freshness",
			"github.com/marmutapp/superbased-observer/internal/scoring",
			"github.com/marmutapp/superbased-observer/internal/compression",
		},
		Reason: "module boundary 24.1",
	})
}
