package export

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins export PURE with ZERO observer imports (guard
// spec 17.1): the 11.4 no-network invariant is structural, a formatter that
// cannot open a socket cannot exfiltrate.
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"os",
			"os/exec",
			"io",
			"io/ioutil",
			"path/filepath",
			"net",
			"net/http",
			"database/sql",
			"github.com/fsnotify/fsnotify",
		},
		ForbiddenPrefixes: []string{
			"github.com/marmutapp/superbased-observer/",
		},
		Reason: "module boundary 17.1",
	})
}
