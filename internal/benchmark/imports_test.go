package benchmark

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/benchmark pure: I/O is injected at the
// store seam and the runner (cmd/observer/benchmark.go), never here.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"os/exec",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/intelligence/cost",
			"github.com/marmutapp/superbased-observer/internal/store",
		},
		Reason: "internal/benchmark must stay pure",
	})
}
