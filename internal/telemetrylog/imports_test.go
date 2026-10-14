package telemetrylog

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoNonStdlibImports pins internal/telemetrylog as the PURE contract
// (CLAUDE.md rule 1, plan section 4.1): standard library only, crucially no
// broker client (nats.go). The adapters and logtest hold every dependency.
func TestNoNonStdlibImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		StdlibOnly: true,
		Reason:     "internal/telemetrylog must stay pure",
	})
}
