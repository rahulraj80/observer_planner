package procmodel

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Neutral pins procmodel as the neutral process model
// (modular packaging phase PB, rev F1): core packages import it, so it is
// standard library only plus internal/platform/crossmount (pure path math).
func TestPackageImports_Neutral(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		Forbidden:  []string{"database/sql", "net/http", "os/exec"},
		StdlibOnly: true,
		Allowed:    []string{"github.com/marmutapp/superbased-observer/internal/platform/crossmount"},
		Reason:     "procmodel is the neutral model; it imports no feature package",
	})
}
