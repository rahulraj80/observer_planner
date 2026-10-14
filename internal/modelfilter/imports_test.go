package modelfilter_test

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/modelfilter as pure string building (CLAUDE.md rule 1): every reader
// composes its fragments into its own query.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{"database/sql", "net/http", "github.com/fsnotify/fsnotify"},
		Reason:    "internal/modelfilter must stay pure",
	})
}
