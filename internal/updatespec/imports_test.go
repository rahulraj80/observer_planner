package updatespec

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_StdlibOnly pins updatespec as a NEUTRAL core package
// (modular packaging phase PB, rev F1): the core config loader imports it,
// so it is standard library only and does no I/O.
func TestPackageImports_StdlibOnly(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		StdlibOnly: true,
		Forbidden:  []string{"database/sql", "net/http", "os/exec"},
		Reason:     "updatespec is a neutral core package",
	})
}
