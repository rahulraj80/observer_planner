package mcpschema

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/mcpschema as a pure leaf (CLAUDE.md module rule 1): no I/O imports
// and no observer package (it extends internal/guard/mcpsec rather than wrapping it, plan 11.4).
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden:         []string{"database/sql", "net/http", "github.com/fsnotify/fsnotify", "github.com/marmutapp/superbased-observer/internal/guard/mcpsec"},
		ForbiddenPrefixes: []string{"github.com/marmutapp/superbased-observer/"},
		Reason:            "mcpschema must stay a leaf",
	})
}
