package notifycfg

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_StdlibOnly pins notifycfg as a NEUTRAL core package (modular packaging PB, rev F1): the core
// config loader imports it, so standard library only, and none of database/sql, net/http, os/exec.
func TestPackageImports_StdlibOnly(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		StdlibOnly: true,
		Forbidden:  []string{"database/sql", "net/http", "os/exec"},
		Reason:     "notifycfg is a neutral core package",
	})
}
