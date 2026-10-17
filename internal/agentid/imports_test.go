package agentid

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImportsBounded pins doc3 2.1/2.2: internal/agentid is pure (the
// store and HTTP seams are injected) and may import only internal/dpop/... of
// this module.
func TestPackageImportsBounded(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"github.com/marmutapp/superbased-observer/internal/orgserver",
			"github.com/marmutapp/superbased-observer/internal/store",
			"github.com/marmutapp/superbased-observer/internal/proxy",
		},
		Custom: func(_, p string) string {
			if strings.HasPrefix(p, "github.com/marmutapp/superbased-observer/") && !(p == "github.com/marmutapp/superbased-observer/internal/dpop" || strings.HasPrefix(p, "github.com/marmutapp/superbased-observer/internal/dpop/")) {
				return "agentid may import only internal/dpop/..."
			}
			return ""
		},
		Reason: "doc3 2.1",
	})
}
