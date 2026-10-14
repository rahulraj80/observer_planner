package cacheobs

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins cacheobs as pure logic shared by every Tier-2
// producer (spec 24.1): I/O and per-tool parsing stay in the calling adapter.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/proxy",
			"github.com/marmutapp/superbased-observer/internal/db",
			"github.com/marmutapp/superbased-observer/internal/watcher",
			"github.com/marmutapp/superbased-observer/internal/hook",
		},
		Reason: "module boundary 24.1",
	})
}
