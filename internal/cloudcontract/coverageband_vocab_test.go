package cloudcontract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// declaredCoverageBands returns the value of every `X CoverageBand = "..."`
// constant in the package's non-test source. It is read from source, a
// second source independent of allCoverageBands, so a band declared but
// never added to the vocabulary (which Valid would then reject on the wire)
// cannot hide.
func declaredCoverageBands(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, s := range gd.Specs {
				vs := s.(*ast.ValueSpec)
				id, ok := vs.Type.(*ast.Ident)
				if !ok || id.Name != "CoverageBand" {
					continue
				}
				for _, v := range vs.Values {
					if bl, ok := v.(*ast.BasicLit); ok && bl.Kind == token.STRING {
						s, _ := strconv.Unquote(bl.Value)
						out = append(out, s)
					}
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// TestCoverageBandVocabularyIsClosed pins the band vocabulary against the
// declared constants (read from source): the two sets must be equal, every
// member must be Valid, and a value outside the set (including a near-miss
// spelling of a real band) must be rejected.
func TestCoverageBandVocabularyIsClosed(t *testing.T) {
	t.Parallel()
	declared := declaredCoverageBands(t)
	if len(declared) < 2 {
		t.Fatalf("found only %d CoverageBand constants in source (%v): the pin would be vacuous", len(declared), declared)
	}
	var vocab []string
	for _, b := range AllCoverageBands() {
		vocab = append(vocab, string(b))
		if !b.Valid() {
			t.Errorf("%q is in AllCoverageBands but reports invalid", b)
		}
	}
	sort.Strings(vocab)
	if strings.Join(declared, ",") != strings.Join(vocab, ",") {
		t.Errorf("declared CoverageBand constants %v != AllCoverageBands %v: add the new band to allCoverageBands (or delete the constant)", declared, vocab)
	}
	for _, d := range declared {
		if !CoverageBand(d).Valid() {
			t.Errorf("declared band %q is rejected by Valid", d)
		}
	}
	unknown := []CoverageBand{"", "partial", "NONE", "very-high", " high", "high ", "Low"}
	for _, b := range unknown {
		if b.Valid() {
			t.Errorf("%q must not be a valid coverage band", b)
		}
	}
}
