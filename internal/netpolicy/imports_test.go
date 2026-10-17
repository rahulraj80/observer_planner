package netpolicy

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/netpolicy as a pure primitive (CLAUDE.md module rule 1): no I/O
// imports and no Observer package at all.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden:         []string{"database/sql", "net/http", "github.com/fsnotify/fsnotify"},
		ForbiddenPrefixes: []string{"github.com/marmutapp/superbased-observer/"},
		Reason:            "netpolicy must not depend on any Observer package",
	})
}
