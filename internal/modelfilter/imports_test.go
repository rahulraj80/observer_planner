package modelfilter_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoForbiddenImports pins the module-boundary discipline (CLAUDE.md #1):
// internal/modelfilter is pure string building. Every reader composes its
// fragments into its own query; the package itself never touches a
// database, the network or the filesystem watcher.
func TestNoForbiddenImports(t *testing.T) {
	forbidden := []string{"database/sql", "net/http", "github.com/fsnotify/fsnotify"}
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, f := range matches {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, imp := range af.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if path == bad {
					t.Errorf("%s imports forbidden %q: internal/modelfilter must stay pure", f, bad)
				}
			}
		}
	}
}
