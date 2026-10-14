package oneshot

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/oneshot pure (CLAUDE.md "Module Boundaries" rule 1).
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{"database/sql", "net/http", "os/exec", "github.com/fsnotify/fsnotify", "github.com/marmutapp/superbased-observer/internal/store", "github.com/marmutapp/superbased-observer/internal/db", "github.com/marmutapp/superbased-observer/internal/intelligence/cost"},
		Reason:    "internal/oneshot is pure",
	})
}
