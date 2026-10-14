package timebucket

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImportsPure pins timebucket pure (CLAUDE.md rule 1): standard
// library only. SQL lives in the callers; this package only renders
// expressions.
func TestPackageImportsPure(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{"database/sql", "net/http", "github.com/fsnotify/fsnotify"},
		Custom: func(_, p string) string {
			if strings.Contains(p, ".") {
				return "non-stdlib; timebucket is stdlib-only"
			}
			return ""
		},
		Reason: "timebucket is pure",
	})
}
