package remoteauth

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoIOImports pins remoteauth as a PURE-LOGIC package (CLAUDE.md module
// rule #1): HTTP glue, SQL persistence and file I/O live in the callers.
func TestNoIOImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"net/http",
			"database/sql",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "remoteauth must stay pure (no net/http, database/sql, fsnotify)",
	})
}
