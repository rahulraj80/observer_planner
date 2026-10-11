package managedconfig

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestImportsAndNoRedirection pins the package's two promises (plan 6.2.2
// rev N2): the decision files are pure, and nothing in the package can be
// redirected by a flag, an environment variable or the config loader.
func TestImportsAndNoRedirection(t *testing.T) {
	everywhere := []string{
		"database/sql", "net/http", "flag",
		"github.com/fsnotify/fsnotify",
		"github.com/spf13/cobra", "github.com/spf13/viper",
		"github.com/marmutapp/superbased-observer/internal/config",
		"github.com/marmutapp/superbased-observer/internal/store",
	}
	pureFiles := map[string]bool{"ceiling.go": true, "acl.go": true, "load.go": true, "doc.go": true}
	pureForbidden := []string{"os", "os/exec", "syscall", "golang.org/x/sys", "unsafe"}
	execAllowed := map[string]bool{"source_darwin.go": true}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			bad := append([]string{}, everywhere...)
			if pureFiles[f] {
				bad = append(bad, pureForbidden...)
			}
			if !execAllowed[f] {
				bad = append(bad, "os/exec")
			}
			for _, b := range bad {
				if p == b || strings.HasPrefix(p, b+"/") {
					t.Errorf("%s imports %s", f, p)
				}
			}
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range []string{"Getenv(", "LookupEnv(", "Environ(", "os.Args", "UserHomeDir(", "UserConfigDir("} {
			if strings.Contains(string(src), needle) {
				t.Errorf("%s uses %s: the ceiling source must be a fixed location nothing can redirect", f, needle)
			}
		}
	}
}
