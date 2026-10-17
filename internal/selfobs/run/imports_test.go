package run

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins run as a pure, OTel-FREE value package (the
// shaping that needs OTel lives in internal/selfobs/shape).
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
			"go.opentelemetry.io",
		},
		Reason: "run must stay OTel-free and infra-free",
	})
}
