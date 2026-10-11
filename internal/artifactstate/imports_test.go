package artifactstate

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestPureImports pins the package pure (CLAUDE.md module rule 1): the
// caller observes, this package decides. No SQL, HTTP, fsnotify, process
// execution or file system access.
func TestPureImports(t *testing.T) {
	forbidden := []string{"database/sql", "net/http", "os", "os/exec", "io/fs", "path/filepath", "github.com/fsnotify/fsnotify"}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), e.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if p == bad {
					t.Errorf("%s imports %s: internal/artifactstate is pure", e.Name(), p)
				}
			}
		}
	}
}
