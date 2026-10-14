package guardfallback

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins the fallback vocabulary pure and leaf (CLAUDE.md
// rule 1): the org server, guard, hook and launcher all share it.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"os",
			"os/exec",
			"github.com/fsnotify/fsnotify",
		},
		ForbiddenPrefixes: []string{
			"github.com/marmutapp/superbased-observer/internal/",
		},
		Reason: "internal/guardfallback must stay pure and leaf",
	})
}
