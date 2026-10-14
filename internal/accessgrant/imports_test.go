package accessgrant

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPurity pins the package free of I/O dependencies (CLAUDE.md #1): the
// org server and the node feed it already-loaded facts.
func TestPurity(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"os/exec",
			"os",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/config",
			"github.com/marmutapp/superbased-observer/internal/guard",
			"github.com/marmutapp/superbased-observer/internal/orgserver",
			"github.com/marmutapp/superbased-observer/internal/orgclient",
		},
		Reason: "accessgrant is pure",
	})
}
