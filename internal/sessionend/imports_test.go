package sessionend

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/sessionend pure (CLAUDE.md section 1,
// spec section 24.1): the node store (internal/store/sessionlifecycle.go)
// loads the facts.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "internal/sessionend must stay pure",
	})
}
