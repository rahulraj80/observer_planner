package components

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoForbiddenImports pins the catalog package pure: it DESCRIBES the
// product and never does I/O. A database/sql, net/http, os/exec or
// fsnotify import here would mean the catalog had started doing something,
// the regression CLAUDE.md module rule 1 forbids. The buildfeat subpackage
// is held to the same rule.
func TestNoForbiddenImports(t *testing.T) {
	forbidden := []string{"database/sql", "net/http", "os/exec", "github.com/fsnotify/fsnotify"}
	for _, dir := range []string{".", "buildfeat", "sbom"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				p := strings.Trim(imp.Path.Value, `"`)
				for _, bad := range forbidden {
					if p == bad || strings.HasPrefix(p, bad+"/") {
						t.Errorf("%s imports %s: internal/components is pure data", path, p)
					}
				}
			}
		}
	}
}
