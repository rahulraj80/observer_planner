package sshforward

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins sshforward to process lifecycle only
// (CLAUDE.md rule 1): os/exec and net are allowed, but no storage, no HTTP
// (it never proxies the forwarded dashboard), no fsnotify, and no observer
// package except the pure internal/sshprofile.
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
		},
		ForbiddenPrefixes: []string{"github.com/marmutapp/superbased-observer/internal/"},
		Except:            []string{"github.com/marmutapp/superbased-observer/internal/sshprofile"},
		Reason:            "sshforward owns process lifecycle only",
	})
}
