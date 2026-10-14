package feed_test

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestNoForbiddenImports pins internal/notify/feed as pure aggregation over facts the caller loaded
// (CLAUDE.md rule 1): no database, network, watcher or disk.
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{"database/sql", "net/http", "github.com/fsnotify/fsnotify", "os"},
		Reason:    "internal/notify/feed must stay pure",
	})
}
