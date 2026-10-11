package updatespec

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackageImports_StdlibOnly pins updatespec as a NEUTRAL core package
// (modular packaging phase PB, rev F1): the core config loader imports it,
// so it must never pull a feature package, a third-party module or I/O
// infrastructure into every distro. Standard library only, and none of
// database/sql, net/http, os/exec.
func TestPackageImports_StdlibOnly(t *testing.T) {
	t.Parallel()
	forbidden := map[string]bool{"database/sql": true, "net/http": true, "os/exec": true}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(cwd, "*.go"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no source files in %s (%v)", cwd, err)
	}
	fset := token.NewFileSet()
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			first := strings.SplitN(p, "/", 2)[0]
			if strings.Contains(first, ".") {
				t.Errorf("%s: import %q is not standard library (updatespec is a neutral core package)", filepath.Base(path), p)
			}
			if forbidden[p] {
				t.Errorf("%s: forbidden import %q (updatespec is pure)", filepath.Base(path), p)
			}
		}
	}
}
