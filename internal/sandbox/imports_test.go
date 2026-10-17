package sandbox

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins sandbox PURE (CLAUDE.md Module Boundaries
// #1): all I/O is injected through Env, and no observer-internal package is
// imported (the per-tool bind DATA crosses the seam as plain []string).
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		Forbidden:         []string{"database/sql", "net/http", "os/exec", "github.com/fsnotify/fsnotify"},
		ForbiddenPrefixes: []string{"github.com/marmutapp/superbased-observer/internal/"},
		Reason:            "sandbox is pure; inject I/O via Env",
	})
}
