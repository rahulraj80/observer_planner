package workspace

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/workspace pure (CLAUDE.md rule 1): it
// decides git argv and destination paths but never runs a subprocess, touches
// a database, calls the network or watches files (I/O is injected, U4/U5).
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"os/exec",
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "internal/workspace must stay pure (git argv/paths only, no subprocess exec)",
	})
}
