package community

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageIsPure pins the community registry pure logic (CLAUDE.md rule 1):
// SQL lives in internal/cloudserver/store, HTTP in internal/cloudserver/api.
func TestPackageIsPure(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "community must stay pure",
	})
}
