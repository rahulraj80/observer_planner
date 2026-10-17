package jetbrainshost

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins jetbrainshost PURE with an allow-list, like
// vscodehost's (CLAUDE.md Module Boundaries #1): os only for
// os.Getenv("APPDATA"), directory listings are injected.
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		AllowOnly: true,
		Allowed: []string{
			"path/filepath",
			"runtime",
			"os",
			"strings",
			"github.com/marmutapp/superbased-observer/internal/models",
			"github.com/marmutapp/superbased-observer/internal/platform/crossmount",
		},
		Reason: "jetbrainshost must stay pure",
	})
}
