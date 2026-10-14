package egress

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins policyfam/egress pure (CLAUDE.md section 1,
// design section 3.1): beyond the admission family's list it must not import
// internal/routing (vocabulary is re-expressed as Plane-A enums).
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/config",
			"github.com/marmutapp/superbased-observer/internal/obs",
			"github.com/marmutapp/superbased-observer/internal/routing",
		},
		Reason: "internal/policyfam/egress must stay pure and routing/obs-free",
	})
}
