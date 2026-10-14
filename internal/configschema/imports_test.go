package configschema

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPackageImports_Bounded pins configschema pure (CLAUDE.md #1): it only
// describes config.Config, so internal/config is its only observer import.
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"os",
			"os/exec",
			"io/fs",
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
		},
		Custom: func(_, p string) string {
			if strings.HasPrefix(p, "github.com/marmutapp/superbased-observer/internal/") && p != "github.com/marmutapp/superbased-observer/internal/config" {
				return "only internal/config is allowed"
			}
			return ""
		},
		Reason: "configschema is pure",
	})
}
