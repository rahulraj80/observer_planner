package termcore

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/termcore as pure vocabulary (CLAUDE.md
// rule 1): no infrastructure and no terminals-component package.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
		},
		Custom: func(_, p string) string {
			if strings.Contains(p, "/internal/term") && !strings.HasSuffix(p, "/internal/termcore") {
				return "termcore must not reach back into the terminals component"
			}
			return ""
		},
		Reason: "internal/termcore must stay pure",
	})
}
