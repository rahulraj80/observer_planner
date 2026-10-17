package integration

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins the registry as pure data + lookups (MOD-1): os/exec
// is forbidden too, because probing a launcher binary belongs at the boundary.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"os/exec",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "internal/integration must stay pure data + lookups",
	})
}
