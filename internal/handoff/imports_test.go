package handoff

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/handoff pure: transcripts arrive via
// internal/handoffsvc and pricing as a plain PriceFunc.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"os",
			"io/ioutil",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/intelligence/cost",
			"github.com/marmutapp/superbased-observer/internal/routing",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/adapter",
		},
		Reason: "internal/handoff must stay pure",
	})
}
