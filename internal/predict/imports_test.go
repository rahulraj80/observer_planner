package predict

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/predict pure (CLAUDE.md section 1,
// spec section 24.1): I/O is injected at the store seam
// (internal/store/predict.go) and the proxy graft.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/intelligence/cost",
		},
		Reason: "internal/predict must stay pure",
	})
}
