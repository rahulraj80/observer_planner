package nodecomponents

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoForbiddenImports pins the package pure (CLAUDE.md #1): it is read by
// the org server (compile before signing) and by every node process,
// including the short-lived hook path, so it does no I/O at all.
func TestNoForbiddenImports(t *testing.T) {
	forbidden := []string{
		"database/sql",
		"net/http",
		"os",
		"os/exec",
		"github.com/fsnotify/fsnotify",
		"github.com/marmutapp/superbased-observer/internal/config",
		"github.com/marmutapp/superbased-observer/internal/obs",
		"github.com/marmutapp/superbased-observer/internal/proxy",
		"github.com/marmutapp/superbased-observer/internal/store",
	}
	fset := token.NewFileSet()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
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
				if path == bad || strings.HasPrefix(path, bad+"/") {
					t.Errorf("%s imports forbidden %q: internal/policyfam/nodecomponents must stay pure", f, bad)
				}
			}
		}
	}
}
