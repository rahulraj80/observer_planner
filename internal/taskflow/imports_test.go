package taskflow

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/taskflow pure (CLAUDE.md rule 1): I/O
// and cost pricing are injected at the store seam
// (internal/store/taskflow.go) and the dashboard consumer.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"os",
			"os/exec",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/intelligence/cost",
		},
		Reason: "internal/taskflow must stay pure",
	})
}
