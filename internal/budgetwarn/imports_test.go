package budgetwarn

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPurity pins the package free of I/O dependencies (CLAUDE.md #1): the
// ladder is a pure function the daemon boundary feeds.
func TestPurity(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"os/exec",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/config",
		},
		Reason: "budgetwarn is pure",
	})
}
