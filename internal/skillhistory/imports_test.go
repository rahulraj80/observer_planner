package skillhistory

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/skillhistory pure (CLAUDE.md rule 1):
// every load happens at the store seam (internal/store/skillhistory.go) and
// every git invocation in internal/skillscan.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"os",
			"os/exec",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/commitlog",
		},
		ForbiddenPrefixes: []string{
			"github.com/marmutapp/superbased-observer/internal/adapter/",
			"modernc.org/sqlite",
		},
		Reason: "internal/skillhistory must stay pure",
	})
}
