package skillscan

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/skillscan as orchestration over
// injected funcs (CLAUDE.md rule 1): the read-only VCS envelope and every
// store read/write arrive through Options.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"os/exec",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/store",
		},
		Reason: "internal/skillscan must stay I/O-injected",
	})
}
