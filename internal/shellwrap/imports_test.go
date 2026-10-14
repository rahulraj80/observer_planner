package shellwrap

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins shellwrap PURE (CLAUDE.md rule 1): no
// infrastructure, no path/filepath (it follows the BUILD host, not the target
// OS), and only internal/integration plus internal/shimguard (the core owner
// of the shim marker since MP-L1E, D-505) among observer packages.
func TestPackageImports_Bounded(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql", "net/http", "os", "os/exec",
			"path/filepath", "github.com/fsnotify/fsnotify",
		},
		ForbiddenPrefixes: []string{"github.com/marmutapp/superbased-observer/internal/"},
		Except: []string{
			"github.com/marmutapp/superbased-observer/internal/integration",
			"github.com/marmutapp/superbased-observer/internal/shimguard",
		},
		Reason: "shellwrap is pure",
	})
}
