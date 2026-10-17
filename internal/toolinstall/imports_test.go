package toolinstall

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/toolinstall as pure planning
// (CLAUDE.md rule 1): the record lives behind internal/store and the spawn
// behind cmd/observer.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"os/exec",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/store",
		},
		Reason: "internal/toolinstall must stay pure",
	})
}
