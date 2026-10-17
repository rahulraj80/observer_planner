package coverage

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins the package pure (CLAUDE.md rule 1): no I/O imports and no observer package
// other than the integration registry (pure data) it derives from.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden:         []string{"database/sql", "net/http", "github.com/fsnotify/fsnotify", "os", "os/exec"},
		ForbiddenPrefixes: []string{"github.com/marmutapp/superbased-observer/"},
		Except:            []string{"github.com/marmutapp/superbased-observer/internal/integration"},
		Reason:            "only internal/integration may be imported from the module",
	})
}
