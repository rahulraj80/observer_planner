package admission

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins the extracted family COMPILER pure (CLAUDE.md
// section 1, Plane-A P0-5 Phase F): reused by orgserver and orgclient, it must
// not pull internal/obs into their graph (tests/invariant/obs_boundary_test.go
// pins the flip side).
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/config",
			"github.com/marmutapp/superbased-observer/internal/obs",
		},
		Reason: "internal/policyfam/admission must stay pure",
	})
}
