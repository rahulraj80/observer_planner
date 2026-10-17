package statusline

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/statusline pure (CLAUDE.md rule 1):
// all I/O (stdin, the daemon HTTP call, the lockfile pre-check) lives at the
// cmd/observer/statusline.go boundary.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"os/exec",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/db",
			"github.com/marmutapp/superbased-observer/internal/intelligence/cost",
		},
		Reason: "internal/statusline is pure",
	})
}
