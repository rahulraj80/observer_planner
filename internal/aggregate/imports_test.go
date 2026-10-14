package aggregate

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins aggregate as pure assembly (design 6.1): the
// SQL read lives in internal/aggregatesource and egress in internal/aggregateclient.
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/orgclient",
			"github.com/marmutapp/superbased-observer/internal/proxy",
			"github.com/marmutapp/superbased-observer/internal/intelligence/cost",
			"github.com/marmutapp/superbased-observer/internal/aggregateclient",
			"github.com/marmutapp/superbased-observer/internal/aggregatesource",
		},
		Reason: "pure-aggregation boundary",
	})
}
