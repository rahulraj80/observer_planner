package procmodel

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackageImports_Neutral pins procmodel as the neutral process model
// (modular packaging phase PB, rev F1): core packages (store, proxy, metrics,
// diag, dashboard, arena) import it, so it must never pull the capture
// runtime (internal/processobs and its backends), a third-party module or
// I/O infrastructure into every distro. The one internal import allowed is
// internal/platform/crossmount (pure path translation for cross-OS matching).
func TestPackageImports_Neutral(t *testing.T) {
	t.Parallel()
	const internalPrefix = "github.com/marmutapp/superbased-observer/internal/"
	allowedInternal := map[string]bool{internalPrefix + "platform/crossmount": true}
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
			if forbidden[p] {
				t.Errorf("%s: forbidden import %q (procmodel is pure)", filepath.Base(path), p)
			}
			if strings.HasPrefix(p, internalPrefix) {
				if !allowedInternal[p] {
					t.Errorf("%s: internal import %q (procmodel is the neutral model; it imports no feature package)", filepath.Base(path), p)
				}
				continue
			}
			if first := strings.SplitN(p, "/", 2)[0]; strings.Contains(first, ".") {
				t.Errorf("%s: third-party import %q (procmodel is standard library only)", filepath.Base(path), p)
			}
		}
	}
}
