package tooltax

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports is the strictest import pin in the repo: tooltax
// imports nothing from this module, not even internal/models, because
// models (and policy, adapters, store) import tooltax and any internal
// import here would be a cycle (see doc.go).
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"github.com/fsnotify/fsnotify",
		},
		ForbiddenPrefixes: []string{"github.com/marmutapp/superbased-observer/"},
		Reason:            "internal/tooltax must stay pure and import nothing from this module",
	})
}
