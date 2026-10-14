package components

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins the catalog pure: it DESCRIBES the product and
// never does I/O (CLAUDE.md rule 1). The buildfeat and sbom subpackages are
// held to the same rule.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Dirs: []string{
			".",
			"buildfeat",
			"sbom",
		},
		ForbiddenTrees: []string{
			"database/sql",
			"net/http",
			"os/exec",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "internal/components is pure data",
	})
}
