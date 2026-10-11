package telemetrywire

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoForbiddenImports pins the package pure: it plans edits on bytes and
// never does I/O of its own (internal/clientwire is the one writer).
func TestNoForbiddenImports(t *testing.T) {
	forbidden := []string{"database/sql", "net/http", "os", "os/exec", "github.com/fsnotify/fsnotify", "github.com/marmutapp/superbased-observer/internal/store"}
	matches, err := filepath.Glob("*.go")
	if err != nil || len(matches) == 0 {
		t.Fatalf("glob: %v (%d files)", err, len(matches))
	}
	fset := token.NewFileSet()
	for _, f := range matches {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if path == bad {
					t.Errorf("%s imports %s", f, path)
				}
			}
		}
	}
}
