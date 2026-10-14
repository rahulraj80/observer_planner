package mcprelay

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImportsBounded pins the reverse-import boundary (doc3 11.7): the node relay never imports
// orgserver, mcpgw or store, and (owning net/http and os/exec by nature) never SQL or fsnotify.
func TestPackageImportsBounded(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{"database/sql", "github.com/fsnotify/fsnotify", "github.com/marmutapp/superbased-observer/internal/orgserver", "github.com/marmutapp/superbased-observer/internal/mcpgw", "github.com/marmutapp/superbased-observer/internal/store"},
		Reason:         "mcprelay reverse-import boundary",
	})
}
