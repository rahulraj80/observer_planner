package accessgrant

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPurity pins the package free of I/O dependencies (CLAUDE.md #1): the
// org server and the node feed it already-loaded facts.
func TestPurity(t *testing.T) {
	banned := []string{
		"database/sql", "net/http", "github.com/fsnotify/fsnotify", "os/exec", "os",
		"github.com/marmutapp/superbased-observer/internal/store",
		"github.com/marmutapp/superbased-observer/internal/config",
		"github.com/marmutapp/superbased-observer/internal/guard",
		"github.com/marmutapp/superbased-observer/internal/orgserver",
		"github.com/marmutapp/superbased-observer/internal/orgclient",
	}
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		af, err := parser.ParseFile(fset, f, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, b := range banned {
				if p == b || strings.HasPrefix(p, b+"/") {
					t.Errorf("%s imports %s", f, p)
				}
			}
		}
	}
}
