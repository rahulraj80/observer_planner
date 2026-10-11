package livecheck

import (
	"flag"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/vendorapi/drivers"
	vs "github.com/marmutapp/superbased-observer/internal/vendorsurface"
	"github.com/marmutapp/superbased-observer/internal/vendorsurface/catalog"
)

var update = flag.Bool("update", false, "rewrite the committed checklist document")

const docPath = "../../../docs/vendor-connections-live-validation.md"

// DriverSurfaces is the checklist input: the catalog rows that have a driver.
func driverSurfaces(t *testing.T) []vs.Surface {
	t.Helper()
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	var out []vs.Surface
	for _, id := range drivers.Surfaces() {
		s, ok := cat.Surface(id)
		if !ok {
			t.Fatalf("driver surface %s is not in the catalog", id)
		}
		out = append(out, s)
	}
	return out
}

// TestChecklistDocIsCurrent: the committed document is exactly Render over
// the driver-backed catalog rows (plan 12A.4: generated and committed).
func TestChecklistDocIsCurrent(t *testing.T) {
	got := Render(Build(driverSurfaces(t)))
	if *update {
		if err := os.WriteFile(docPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if string(want) != got {
		t.Fatal("docs/vendor-connections-live-validation.md is stale: run go test ./internal/vendorsurface/livecheck/ -run TestChecklistDocIsCurrent -update")
	}
}

// TestChecklistNeverClaimsLive: every item names its rungs, and the document
// says no surface is live-validated while no supply has left live_pending.
func TestChecklistNeverClaimsLive(t *testing.T) {
	items := Build(driverSurfaces(t))
	if len(items) == 0 {
		t.Fatal("no driver-backed surface: the checklist is vacuous")
	}
	doc := Render(items)
	for _, it := range items {
		if len(it.Steps) == 0 || len(it.Scenarios) != 5 || len(it.Receipt) == 0 {
			t.Fatalf("%s: incomplete checklist %+v", it.Surface, it)
		}
		for _, s := range it.Supplies {
			if s.Live != string(vs.LivePending) {
				t.Fatalf("%s %s is %s: update the document's opening claim", it.Surface, s.Class, s.Live)
			}
		}
	}
	if !strings.Contains(doc, "No surface is live-validated") {
		t.Fatal("the document lost its honesty line")
	}
	if strings.ContainsAny(doc, "\u2013\u2014") {
		t.Fatal("the document carries an en or em dash")
	}
}

// TestNoForbiddenImports: the generator is pure.
func TestNoForbiddenImports(t *testing.T) {
	matches, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, f := range matches {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if p == "os" || p == "database/sql" || p == "net/http" || strings.Contains(p, "internal/orgserver") {
				t.Errorf("%s imports %s: livecheck must stay pure", f, p)
			}
		}
	}
}
