package otlpvendor

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins the mapping table pure (CLAUDE.md module rule
// 1): no SQL, HTTP, fsnotify, Plane A (internal/obs) or store import; the
// daemon glue owns every write.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/obs",
		},
		Reason: "internal/otlpvendor must stay pure",
	})
}
