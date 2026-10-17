package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// goCopyDashDirs are the Go packages whose string literals are user-visible
// copy (CLI help, errors, doctor and Health rows) and must carry no em or en
// dash (the operator's no-dash copy rule; T-472, T-473, following the I7
// sweep 51a205e81). Comments are not copy and are not checked. Add a package
// here once its literals are clean.
var goCopyDashDirs = []string{
	"cmd/observer",
	// Doctor checks: rendered as CLI lines and as Health page rows.
	"internal/diag",
}

// noValueGlyph is the design system's missing-value cell (a bare em dash with
// no word on either side). D-273 item 5 exempts it from the no-dash rule.
// The dashes are built from code points so this source carries none.
const (
	emDash       = string(rune(0x2014))
	enDash       = string(rune(0x2013))
	noValueGlyph = emDash
)

func init() { register("go-copy-dash", goCopyDash) }

// goCopyDash flags any em or en dash inside a string literal of a non-test
// file in goCopyDashDirs, except the bare no-value glyph. Byte-pinned or
// signed text does not live in these packages; if it ever must, give it its
// own named exemption here rather than a dash.
func goCopyDash(root string) []string {
	var out []string
	for _, rel := range goCopyDashDirs {
		dir := filepath.Join(root, filepath.FromSlash(rel))
		entries, err := os.ReadDir(dir)
		if err != nil {
			out = append(out, fmt.Sprintf("%s: %v", rel, err))
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			fs := token.NewFileSet()
			f, err := parser.ParseFile(fs, path, nil, 0)
			if err != nil {
				out = append(out, fmt.Sprintf("%s: %v", path, err))
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				bl, ok := n.(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					return true
				}
				v, err := strconv.Unquote(bl.Value)
				if err != nil {
					v = bl.Value
				}
				if v == noValueGlyph || !strings.ContainsAny(v, enDash+emDash) {
					return true
				}
				pos := fs.Position(bl.Pos())
				out = append(out, fmt.Sprintf("%s/%s:%d: string literal carries an em or en dash; use ASCII punctuation (the bare %q no-value glyph is the only exemption)",
					rel, name, pos.Line, noValueGlyph))
				return true
			})
		}
	}
	return out
}
