package projectroi

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/projectroi pure (CLAUDE.md section 1,
// spec section 24.1, doc.go): loading and git run in the callers
// (internal/store/projectroi.go, internal/commitlog, the dashboard handler).
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"os/exec",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/commitlog",
		},
		ForbiddenPrefixes: []string{
			"github.com/marmutapp/superbased-observer/internal/adapter/",
			"modernc.org/sqlite",
		},
		Reason: "internal/projectroi must stay pure",
	})
}
