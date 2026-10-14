package otelsink

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins that otelsink, the gateway client, never
// imports internal/obs (the reverse-import boundary).
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		ForbiddenTrees: []string{
			"github.com/marmutapp/superbased-observer/internal/obs",
		},
		Reason: "otelsink must not import internal/obs",
	})
}
