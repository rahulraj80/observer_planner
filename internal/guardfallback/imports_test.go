package guardfallback

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoForbiddenImports pins the module boundary (CLAUDE.md rule 1): the
// fallback vocabulary is shared by the org server, the guard, the hook and
// the launcher, so it must never learn about storage, transport or the
// filesystem.
func TestNoForbiddenImports(t *testing.T) {
	forbidden := []string{"database/sql", "net/http", "os", "os/exec", "github.com/fsnotify/fsnotify"}
	fset := token.NewFileSet()
	matches, err := filepath.Glob("*.go")
	if err != nil || len(matches) == 0 {
		t.Fatalf("glob: %v (%d files)", err, len(matches))
	}
	for _, f := range matches {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, perr := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatalf("parse %s: %v", f, perr)
		}
		for _, imp := range af.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			bad := strings.HasPrefix(path, "github.com/marmutapp/superbased-observer/internal/")
			for _, b := range forbidden {
				bad = bad || path == b
			}
			if bad {
				t.Errorf("%s imports %q: internal/guardfallback must stay pure and leaf", f, path)
			}
		}
	}
}
