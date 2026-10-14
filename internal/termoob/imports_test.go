package termoob

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/termoob as the pure wire protocol
// (CLAUDE.md rule 1): the inherited-FD transport is injected as an
// io.Reader/io.Writer, never opened here, so not even os is allowed.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"os",
		},
		Reason: "internal/termoob must stay pure",
	})
}
