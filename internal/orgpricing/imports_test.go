package orgpricing

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestOrgPricingImportsStayPure pins the package PURE: the daemon and a one-shot CLI must reach the SAME answer,
// and a package that could read a database or a clock could reach two.
func TestOrgPricingImportsStayPure(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{"database/sql", "net/http", "os/exec", "github.com/fsnotify/fsnotify", "github.com/marmutapp/superbased-observer/internal/store", "github.com/marmutapp/superbased-observer/internal/orgclient", "github.com/marmutapp/superbased-observer/internal/intelligence/cost"},
		Reason:    "this package resolves a rule, it does not fetch anything",
	})
}
