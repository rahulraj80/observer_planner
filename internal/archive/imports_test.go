package archive

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/archive pure: hot SQL lives in
// internal/store, cold SQL in internal/archivestore, and neither may leak its
// schema types across the seam.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/archivestore",
			"github.com/marmutapp/superbased-observer/internal/db",
		},
		Reason: "internal/archive must stay pure",
	})
}
