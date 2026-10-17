package cachewarm

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/cachewarm pure: cache rows come from the
// store seam and rates arrive as plain RatePair values, never via cost.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/intelligence/cost",
		},
		Reason: "internal/cachewarm must stay pure",
	})
}
