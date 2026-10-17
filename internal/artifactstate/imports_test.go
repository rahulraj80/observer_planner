package artifactstate

import (
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestPureImports pins the package pure (CLAUDE.md rule 1): the caller
// observes, this package decides. No SQL, HTTP, fsnotify, exec or file system.
func TestPureImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{
			"database/sql",
			"net/http",
			"os",
			"os/exec",
			"io/fs",
			"path/filepath",
			"github.com/fsnotify/fsnotify",
		},
		Reason: "internal/artifactstate is pure",
	})
}
