package cloudpop

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins cloudpop stdlib-only (plan 6 CI-P2): both sides
// of the wire depend on it, so it stays a leaf with no I/O surface.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"net",
			"os",
			"io/fs",
			"os/exec",
			"github.com/fsnotify/fsnotify",
		},
		ForbiddenPrefixes: []string{
			"github.com/marmutapp/superbased-observer/",
		},
		Reason: "cloudpop must be stdlib-only",
	})
}
