package loc

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/loc pure (plan 1, 3.1): all I/O is injected
// by its callers, and it must not drift onto internal/codeintel's language table.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"os/exec",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/models",
		},
		ForbiddenPrefixes: []string{
			"github.com/marmutapp/superbased-observer/internal/adapter/",
			"github.com/marmutapp/superbased-observer/internal/codeintel",
			"modernc.org/sqlite",
		},
		Reason: "internal/loc must stay pure",
	})
}
