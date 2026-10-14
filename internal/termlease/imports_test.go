package termlease

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/termlease as a pure authorization and
// policy primitive (CLAUDE.md rule 1): its dependencies are injected
// interfaces, never config, the terminal packages or the store.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/config",
			"github.com/marmutapp/superbased-observer/internal/remoteauth",
			"github.com/marmutapp/superbased-observer/internal/termsession",
			"github.com/marmutapp/superbased-observer/internal/store",
		},
		Reason: "internal/termlease must stay pure",
	})
}
