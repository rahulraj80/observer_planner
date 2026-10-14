package guilaunch

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins guilaunch PURE: host facts arrive as data on
// Inputs and it spawns nothing; internal/integration (registry data) is its only
// observer import.
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"os",
			"os/exec",
			"github.com/fsnotify/fsnotify",
		},
		Custom: func(_, p string) string {
			if strings.HasPrefix(p, "github.com/marmutapp/superbased-observer/internal/") && p != "github.com/marmutapp/superbased-observer/internal/integration" {
				return "only internal/integration allowed"
			}
			return ""
		},
		Reason: "guilaunch is pure",
	})
}
