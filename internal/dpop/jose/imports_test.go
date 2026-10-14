package jose

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/dpop/jose stdlib-only and a leaf
// (doc3 2.1/2.2).
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"net",
			"os",
			"os/exec",
			"io/fs",
			"github.com/fsnotify/fsnotify",
		},
		ForbiddenPrefixes: []string{
			"github.com/marmutapp/superbased-observer/",
		},
		Custom: func(_, p string) string {
			if strings.Contains(p, ".") {
				return "third-party import; jose must be stdlib-only"
			}
			return ""
		},
		Reason: "jose must stay pure",
	})
}
