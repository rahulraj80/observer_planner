package sessionmsg

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/sessionmsg pure (CLAUDE.md rule 1):
// it is the derivation shared by node and org, and every caller loads its
// own rows and passes plain slices in.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "internal/sessionmsg must stay pure",
	})
}
