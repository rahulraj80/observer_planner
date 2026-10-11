package cfgfmt

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoForbiddenImports pins the pure-engine boundary: cfgfmt turns bytes
// into bytes. Reading, locking, backing up and writing vendor files is the
// caller's job (plan section 5.2 step 8), so no file, database, network or
// watcher package may appear here.
func TestNoForbiddenImports(t *testing.T) {
	forbidden := []string{"os", "os/exec", "database/sql", "net/http", "github.com/fsnotify/fsnotify"}
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	scanned := 0
	for _, f := range matches {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		scanned++
		for _, imp := range af.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if p == bad {
					t.Errorf("%s imports %q: keep internal/cfgfmt pure", f, p)
				}
			}
		}
	}
	if scanned == 0 {
		t.Fatal("no source files scanned: the pin would be vacuous")
	}
}
