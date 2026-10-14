package keys

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins keys as a pure OTel-free leaf: the single
// source of truth even the org-server classify code may import without a
// cycle.
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"go.opentelemetry.io",
		},
		Reason: "keys is a pure OTel-free leaf",
	})
}
