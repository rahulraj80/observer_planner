package emit

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackageImports_Bounded pins that emit is the NEUTRAL sink contract
// (D-436): it never imports internal/obs (the reverse-import boundary), and
// it never imports the OpenTelemetry SDK or internal/exporter/otel, which
// belong to the export.otel component (the otelsink subpackage). Anything
// else it may import is the neutral run model and the standard library.
func TestPackageImports_Bounded(t *testing.T) {
	t.Parallel()

	forbidden := []string{
		"github.com/marmutapp/superbased-observer/internal/obs",
		"github.com/marmutapp/superbased-observer/internal/exporter",
		"go.opentelemetry.io",
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(cwd, "*.go"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("no source files in %s", cwd)
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
			for _, bad := range forbidden {
				if p == bad || strings.HasPrefix(p, bad+"/") {
					t.Errorf("%s: forbidden import %q (emit is the neutral sink contract; the OTel sink lives in emit/otelsink)", filepath.Base(path), p)
				}
			}
		}
	}
}
