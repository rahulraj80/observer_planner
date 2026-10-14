package surfaceenrich

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins surfaceenrich to a closed allow-list
// (CLAUDE.md rule 1): store seams and the filesystem are injected funcs, so
// only this stdlib plus models and the two platform helpers may be imported.
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		AllowOnly: true,
		Allowed: []string{
			"context",
			"log/slog",
			"os",
			"path/filepath",
			"strings",
			"sync",
			"time",
			"github.com/marmutapp/superbased-observer/internal/models",
			"github.com/marmutapp/superbased-observer/internal/platform/crossmount",
			"github.com/marmutapp/superbased-observer/internal/platform/jetbrainshost",
		},
		Reason: "inject it instead",
	})
}
