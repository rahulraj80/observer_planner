package invariant

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// TestComponentsL1ModuleAbsence is the L1 proof for a component that owns
// no package of its own, only third-party modules its host package imports
// from a feature-owned file (ui.export-xlsx: excelize, imported only by
// internal/intelligence/dashboard/export.go; lane MP-L1D). A row that
// claims L1 must leave every one of its DirectModules out of its binary's
// closure when built with its omit tag. Rows with packages are proved by
// TestComponentsL1PackageAbsence instead.
func TestComponentsL1ModuleAbsence(t *testing.T) {
	root := repoRoot(t)
	tagged := buildTagsInTree(t, root)
	for _, c := range components.All() {
		if !c.Levels.Has(components.L1) || len(c.Packages) > 0 || len(c.DirectModules) == 0 {
			continue
		}
		if !dirExists(root, c.Binary.CmdPath()) {
			continue // binary stripped from this tree (public tree)
		}
		if !tagged[c.BuildTag()] {
			t.Errorf("%s claims L1 but no file names its omit tag %s", c.ID, c.BuildTag())
			continue
		}
		for _, goos := range binaryGOOS(c.Binary) {
			g := goListDeps(t, c.Binary.CmdPath(), goos, c.BuildTag())
			for full := range g.pkgs {
				for _, m := range c.DirectModules {
					if full == m || strings.HasPrefix(full, m+"/") {
						t.Errorf("%s claims L1 but %s (module %s) stays in %s with -tags %s (%s)", c.ID, full, m, c.Binary.CmdPath(), c.BuildTag(), goos)
					}
				}
			}
		}
	}
}
