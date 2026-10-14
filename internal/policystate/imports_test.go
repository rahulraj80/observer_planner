package policystate

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/policystate pure (CLAUDE.md section 1,
// spec section 24.1): all I/O is injected at the reader boundary
// (cmd/observer/policystate_wire.go) and the store seams.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "internal/policystate must stay pure",
	})
}
