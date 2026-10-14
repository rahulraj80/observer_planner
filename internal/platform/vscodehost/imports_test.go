package vscodehost

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins vscodehost PURE with an allow-list
// (CLAUDE.md Module Boundaries #1): os only for os.Getenv("APPDATA"); any
// other import, stdlib or internal, fails.
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		AllowOnly: true,
		Allowed: []string{
			"path/filepath",
			"runtime",
			"os",
			"strings",
			"github.com/marmutapp/superbased-observer/internal/platform/crossmount",
		},
		Reason: "vscodehost must stay pure",
	})
}
