package quiesce

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins the module boundary (CLAUDE.md section 1): this
// package counts and waits and must never learn what a request, a PTY or an
// update IS, so the drain rule stays unit-testable without a live proxy.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"net",
			"os/exec",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "internal/quiesce must stay pure",
	})
}
