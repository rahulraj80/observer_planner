package toolresolve

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins toolresolve PURE (CLAUDE.md rule 1): all
// I/O is injected through Env, so not even os is allowed, and the only
// observer dependency is internal/integration (the data it dispatches on).
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"os",
			"os/exec",
			"github.com/fsnotify/fsnotify",
		},
		ForbiddenPrefixes: []string{"github.com/marmutapp/superbased-observer/internal/"},
		Except:            []string{"github.com/marmutapp/superbased-observer/internal/integration"},
		Reason:            "toolresolve is pure, inject I/O via Env",
	})
}
