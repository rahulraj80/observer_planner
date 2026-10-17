package cfgfmt

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins the pure-engine boundary: cfgfmt turns bytes into
// bytes; reading and writing vendor files is the caller's job (plan 5.2 step 8).
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"os",
			"os/exec",
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "keep internal/cfgfmt pure",
	})
}
