package pluginshim

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoForbiddenImports pins pluginshim as a render + place package: no
// SQL, no HTTP, no fsnotify, no subprocess, and no module-internal import
// (the caller passes the subscribed events in).
func TestNoForbiddenImports(t *testing.T) {
	const modulePrefix = "github.com/marmutapp/superbased-observer/"
	forbidden := []string{"database/sql", "net/http", "github.com/fsnotify/fsnotify", "os/exec"}
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, f := range matches {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		checked++
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, imp := range af.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if p == bad {
					t.Errorf("%s imports forbidden %q", f, bad)
				}
			}
			if strings.HasPrefix(p, modulePrefix) {
				t.Errorf("%s imports %q: pluginshim imports nothing from this module", f, p)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no non-test files checked")
	}
}
