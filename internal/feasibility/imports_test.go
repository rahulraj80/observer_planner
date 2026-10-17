package feasibility

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestStdlibOnly pins the package pure (CLAUDE.md rule 1): it decides from
// caller-supplied data, so the node, org server, CLI and HTTP surfaces share it.
func TestStdlibOnly(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"os",
			"os/exec",
		},
		ForbiddenPrefixes: []string{
			"github.com/",
		},
		Reason: "internal/feasibility is pure",
	})
}
