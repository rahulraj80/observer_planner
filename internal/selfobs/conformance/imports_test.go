package conformance

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins conformance's DIRECT imports: never
// internal/obs (the reverse-import boundary), database/sql or fsnotify.
// net/http is deliberately not forbidden (emit pulls it in transitively).
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"github.com/marmutapp/superbased-observer/internal/obs",
			"database/sql",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "conformance must not import internal/obs, database/sql, or fsnotify",
	})
}
