package update

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/update as pure data and logic
// (CLAUDE.md rule 1): no transport (ruling R8, the enrolled org server is
// the only update host and the egress claim depends on it), no process
// control (handshake and probes live in cmd/observer), no storage (W3).
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"net",
			"os/exec",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "internal/update must stay pure (no transport, no storage, no process control)",
	})
}
