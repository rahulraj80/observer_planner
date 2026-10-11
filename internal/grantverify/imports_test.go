package grantverify

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPureImports pins the package's purity: the I/O (rows, key material)
// stays with the callers.
func TestPureImports(t *testing.T) {
	forbidden := []string{"database/sql", "net/http", "github.com/fsnotify/fsnotify", "/internal/store", "/internal/orgclient"}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), f, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range parsed.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if p == bad || strings.HasSuffix(p, bad) {
					t.Errorf("%s imports %s", f, p)
				}
			}
		}
	}
}
