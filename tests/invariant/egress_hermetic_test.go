package invariant

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// egress_hermetic_test.go pins T-668 (D-426): every vendor egress client a
// test builds is hermetic. The vendorconn egress seam is the only outbound
// vendor HTTP path, and once a red (mutation) run of its tests dialed a real
// GitHub address because the test used the system resolver and dialer. Now
// every egress.New in a _test.go file must take its options from a helper
// named hermeticOptions, and that helper must set both the Resolver and the
// Dial seam (to internal/testsupport/hermeticnet), so a test can only ever
// reach a listener inside its own process.

const egressPkg = modulePath + "internal/orgserver/vendorconn/egress"

func TestEgressTestsAreHermetic(t *testing.T) {
	root := filepath.Join("..", "..")
	skipInPublicTree(t, root, "internal/orgserver/vendorconn")
	type pkgState struct {
		calls    []string
		newCalls int
		helperOK bool
	}
	pkgs := map[string]*pkgState{}
	total := 0
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(filepath.Dir(path))
		inEgress := strings.HasSuffix(dir, "internal/orgserver/vendorconn/egress")
		alias := ""
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if p != egressPkg {
				continue
			}
			alias = "egress"
			if imp.Name != nil {
				alias = imp.Name.Name
			}
		}
		if alias == "" && !inEgress {
			return nil
		}
		st := pkgs[dir]
		if st == nil {
			st = &pkgState{}
			pkgs[dir] = st
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncDecl:
				if x.Name.Name == "hermeticOptions" && x.Body != nil {
					st.helperOK = assignsField(x.Body, "Resolver") && assignsField(x.Body, "Dial")
				}
			case *ast.CallExpr:
				if !isEgressNew(x, alias, inEgress) {
					return true
				}
				total++
				st.newCalls++
				pos := fset.Position(x.Pos())
				ok := len(x.Args) == 1
				if ok {
					inner, isCall := x.Args[0].(*ast.CallExpr)
					ok = false
					if isCall {
						if id, isIdent := inner.Fun.(*ast.Ident); isIdent && id.Name == "hermeticOptions" {
							ok = true
						}
					}
				}
				if !ok {
					st.calls = append(st.calls, pos.String())
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if total == 0 {
		t.Fatal("no egress.New call found in any test: the guard would be vacuous")
	}
	// Both packages that build egress clients in tests today must be seen,
	// or an import-path or parsing slip would make the guard vacuous.
	for _, want := range []string{"internal/orgserver/vendorconn/egress", "internal/orgserver/vendorconn/runner"} {
		seen := false
		for dir, st := range pkgs {
			if strings.HasSuffix(dir, want) && st.newCalls > 0 {
				seen = true
			}
		}
		if !seen {
			t.Errorf("no egress.New call seen in %s: the guard would be vacuous there", want)
		}
	}
	for dir, st := range pkgs {
		for _, c := range st.calls {
			t.Errorf("%s: an egress client built without hermeticOptions(...): a test could reach the network (T-668)", c)
		}
		if st.newCalls > 0 && !st.helperOK {
			t.Errorf("%s: hermeticOptions must set both Resolver and Dial (internal/testsupport/hermeticnet)", dir)
		}
	}
}

func isEgressNew(c *ast.CallExpr, alias string, inEgress bool) bool {
	switch f := c.Fun.(type) {
	case *ast.SelectorExpr:
		x, ok := f.X.(*ast.Ident)
		return ok && alias != "" && x.Name == alias && f.Sel.Name == "New"
	case *ast.Ident:
		return inEgress && f.Name == "New"
	}
	return false
}

func assignsField(body *ast.BlockStmt, field string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		a, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, l := range a.Lhs {
			if s, ok := l.(*ast.SelectorExpr); ok && s.Sel.Name == field {
				found = true
			}
		}
		return true
	})
	return found
}
