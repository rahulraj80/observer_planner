package costtext

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoForbiddenImports pins the package's purity (CLAUDE.md "Module
// Boundaries" #1): the standard library's text and JSON packages only, no
// I/O beyond the io.Writer a caller hands in, and no internal package.
func TestNoForbiddenImports(t *testing.T) {
	allowed := map[string]bool{
		"bytes": true, "encoding/json": true, "io": true, "regexp": true, "strconv": true, "strings": true,
	}
	fset := token.NewFileSet()
	matches, err := filepath.Glob("*.go")
	if err != nil || len(matches) == 0 {
		t.Fatalf("glob: %v (%d files)", err, len(matches))
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
			if !allowed[path] {
				t.Errorf("%s imports %q; costtext stays a pure text package", f, path)
			}
		}
	}
}
