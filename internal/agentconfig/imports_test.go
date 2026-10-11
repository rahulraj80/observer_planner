package agentconfig

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoForbiddenImports pins the catalogue as pure data + lookups: no
// file, database, network or watcher package (the reading / writing of
// vendor files belongs to the boundary that calls internal/cfgfmt).
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
					t.Errorf("%s imports %q: keep internal/agentconfig pure", f, p)
				}
			}
		}
	}
	if scanned == 0 {
		t.Fatal("no source files scanned")
	}
}
