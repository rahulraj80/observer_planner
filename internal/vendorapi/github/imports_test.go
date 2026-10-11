package github

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoForbiddenImports pins the module-boundary discipline (CLAUDE.md rule
// 1, enterprise vendor API plan sections 3 and 6.3): a vendor driver is
// pure. It must not import database/sql, net/http, os/exec or fsnotify, nor
// any org-server package (SQL lives in the vendorconn store facade, outbound
// vendor HTTP only in the egress seam).
func TestNoForbiddenImports(t *testing.T) {
	forbidden := []string{
		"database/sql",
		"net/http",
		"os/exec",
		"github.com/fsnotify/fsnotify",
		"github.com/marmutapp/superbased-observer/internal/orgserver",
	}
	fset := token.NewFileSet()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	scanned := 0
	for _, f := range matches {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		scanned++
		for _, imp := range af.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if path == bad || strings.HasPrefix(path, bad+"/") {
					t.Errorf("%s imports forbidden %q: this package must stay pure", f, bad)
				}
			}
		}
	}
	if scanned == 0 {
		t.Fatal("no non-test .go files scanned: the pin would be vacuous")
	}
}
