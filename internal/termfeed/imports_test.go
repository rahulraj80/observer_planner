package termfeed

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/termfeed as a pure in-process
// primitive (CLAUDE.md rule 1) with no dependency on the terminal packages
// (F4), so it stays a generic feed producers are wired into.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/termsession",
			"github.com/marmutapp/superbased-observer/internal/termrun",
			"github.com/marmutapp/superbased-observer/internal/termoob",
		},
		Reason: "internal/termfeed must stay pure and terminal-package-free",
	})
}
