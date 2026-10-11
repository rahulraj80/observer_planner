package feasibility

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestStdlibOnly pins the package pure (CLAUDE.md module rule 1): it decides
// from data the caller supplies and never does I/O, so the node, the org
// server, the CLI and every HTTP surface share it.
func TestStdlibOnly(t *testing.T) {
	forbidden := []string{"database/sql", "net/http", "os", "os/exec", "github.com/"}
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
				if p == bad || strings.HasPrefix(p, bad) && strings.HasSuffix(bad, "/") || strings.HasPrefix(p, bad+"/") {
					t.Errorf("%s imports %s: internal/feasibility is pure", e.Name(), p)
				}
			}
		}
	}
}
