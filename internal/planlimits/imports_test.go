package planlimits

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/planlimits pure (CLAUDE.md "Module
// Boundaries" rule 1): every network call lives in internal/planlimitsnet,
// every vendor-local read in internal/planlimitslocal, every SQL statement in
// internal/store/planlimits.go.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net",
			"net/http",
			"os",
			"os/exec",
			"github.com/fsnotify/fsnotify",
		},
		ForbiddenPrefixes: []string{"github.com/marmutapp/superbased-observer/"},
		Reason:            "internal/planlimits must stay pure",
	})
}
