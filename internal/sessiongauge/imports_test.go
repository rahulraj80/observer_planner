package sessiongauge

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/sessiongauge pure (CLAUDE.md section 1):
// derivation logic shared by node and org; every caller loads its own rows and
// passes plain slices in.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "internal/sessiongauge must stay pure",
	})
}
