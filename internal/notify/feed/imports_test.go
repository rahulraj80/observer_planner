package feed_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoForbiddenImports pins the module-boundary discipline (CLAUDE.md #1):
// internal/notify/feed is pure aggregation over facts the caller loaded. It
// never touches a database, the network, the filesystem watcher or the disk.
func TestNoForbiddenImports(t *testing.T) {
	forbidden := []string{"database/sql", "net/http", "github.com/fsnotify/fsnotify", "os"}
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
					t.Errorf("%s imports forbidden %q: internal/notify/feed must stay pure", f, bad)
				}
			}
		}
	}
}
