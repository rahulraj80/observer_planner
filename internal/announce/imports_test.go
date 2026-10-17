package announce

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/announce pure with no transport:
// announcements ride existing channels and never add an outbound call (plan
// 0/6), so net and net/http are forbidden here.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"net",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "internal/announce must stay pure, no transport",
	})
}
