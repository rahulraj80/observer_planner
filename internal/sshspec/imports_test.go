package sshspec

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins sshspec PURE (CLAUDE.md rule 1): it
// composes an argv and never runs one. os is allowed (ValidateWithFS stats
// the key path); no observer-internal package may be imported.
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"os/exec",
			"github.com/fsnotify/fsnotify",
		},
		ForbiddenPrefixes: []string{"github.com/marmutapp/superbased-observer/internal/"},
		Reason:            "sshspec is pure",
	})
}
