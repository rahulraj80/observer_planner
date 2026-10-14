package codexpatch

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins codexpatch pure logic over a program string, and
// forbids importing any adapter: codexpatch was extracted OUT of the codex
// adapter so two owners share one decoder, and the adapters are its callers.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
		},
		ForbiddenPrefixes: []string{
			"github.com/marmutapp/superbased-observer/internal/adapter/",
		},
		Reason: "codexpatch is the shared decoder the adapters call, never the other way round",
	})
}
