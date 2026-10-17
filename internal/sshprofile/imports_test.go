package sshprofile

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins sshprofile PURE (CLAUDE.md rule 1): it
// composes an argv and never runs one. os is allowed (ValidateWithFS stats
// the key path); the only observer import is the neutral internal/sshspec.
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
		Except:            []string{"github.com/marmutapp/superbased-observer/internal/sshspec"},
		Reason:            "sshprofile is pure",
	})
}
