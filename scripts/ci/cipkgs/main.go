// Command cipkgs decides which Go test packages a CI run tests, and which of
// them run under the race detector (D-609). It is the one owner of both
// rules; ci.yml and scripts/ci/go-test-run.sh only call it.
//
//	go run ./scripts/ci/cipkgs race
//	    Module-relative paths of the packages that run with -race on a push:
//	    every package whose Go files (tests included) use concurrency (a go
//	    statement, select, a channel type or operation, or an import of sync
//	    or sync/atomic). Everything else runs without -race on a push. The
//	    nightly and the release gate run the whole suite with -race.
//
//	go run ./scripts/ci/cipkgs affected <base-sha> <head-sha>
//	    Module-relative paths of the test packages a push from base to head
//	    can affect: the packages holding a changed file plus every package
//	    whose test dependency closure (go list -test -deps) contains one.
//	    Prints the single word ALL when the run must test everything: an
//	    unknown base, a change to a shared input (go.mod, go.sum, ci.yml,
//	    scripts/ci, top-level testdata, any migrations directory, ...), or a
//	    changed file that belongs to no package and is not on the
//	    known-unread list (a test may read it from disk).
//
// The rules are tables (fallbackRules, unreadRules, concurrency kinds), one
// test row each in main_test.go.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "cipkgs:", err)
		os.Exit(2)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: cipkgs race | cipkgs affected <base> <head>")
	}
	root, module, err := moduleRoot()
	if err != nil {
		return err
	}
	switch args[0] {
	case "race":
		pkgs, err := listPackages(root)
		if err != nil {
			return err
		}
		var race []string
		for _, p := range pkgs {
			ok, err := usesConcurrency(filepath.Join(root, filepath.FromSlash(p.rel)), p.files)
			if err != nil {
				return err
			}
			if ok {
				race = append(race, p.rel)
			}
		}
		sort.Strings(race)
		for _, r := range race {
			fmt.Fprintln(out, r)
		}
		return nil
	case "affected":
		if len(args) != 3 {
			return errors.New("usage: cipkgs affected <base> <head>")
		}
		res, err := affected(root, module, args[1], args[2])
		if err != nil {
			return err
		}
		for _, r := range res {
			fmt.Fprintln(out, r)
		}
		return nil
	default:
		return fmt.Errorf("unknown mode %q", args[0])
	}
}

// ---------------------------------------------------------------- packages

type pkgInfo struct {
	rel   string   // module-relative import path ("." for the root)
	files []string // every Go file name in the directory (tests included)
}

func moduleRoot() (root, module string, err error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}|{{.Path}}").Output()
	if err != nil {
		return "", "", fmt.Errorf("go list -m: %w", err)
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("go list -m: unexpected %q", out)
	}
	return parts[0], parts[1], nil
}

type listed struct {
	ImportPath     string
	Dir            string
	GoFiles        []string
	TestGoFiles    []string
	XTestGoFiles   []string
	IgnoredGoFiles []string
	Deps           []string
	ForTest        string
}

func goListJSON(root string, args ...string) ([]listed, error) {
	cmd := exec.Command("go", append([]string{"list", "-e", "-json"}, args...)...)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list %s: %w\n%s", strings.Join(args, " "), err, stderr.String())
	}
	var res []listed
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var l listed
		if err := dec.Decode(&l); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode go list: %w", err)
		}
		res = append(res, l)
	}
	return res, nil
}

func listPackages(root string) ([]pkgInfo, error) {
	ls, err := goListJSON(root, "./...")
	if err != nil {
		return nil, err
	}
	var res []pkgInfo
	for _, l := range ls {
		rel, err := filepath.Rel(root, l.Dir)
		if err != nil {
			return nil, err
		}
		files := append(append(append([]string{}, l.GoFiles...), l.TestGoFiles...), l.XTestGoFiles...)
		files = append(files, l.IgnoredGoFiles...) // build-tag variants count too
		res = append(res, pkgInfo{rel: filepath.ToSlash(rel), files: files})
	}
	return res, nil
}

// ---------------------------------------------------------------- race rule

// concurrencyImports are the imports that mark a package as concurrent.
var concurrencyImports = map[string]bool{"sync": true, "sync/atomic": true}

// usesConcurrency reports whether any of the files uses a concurrency
// construct: a go statement, a select, a channel type, a send, a receive, or
// an import of sync / sync/atomic.
func usesConcurrency(dir string, files []string) (bool, error) {
	fset := token.NewFileSet()
	for _, name := range files {
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			// A file that does not parse cannot be judged; run it with -race.
			return true, nil //nolint:nilerr // fail toward more checking
		}
		if fileUsesConcurrency(f) {
			return true, nil
		}
	}
	return false, nil
}

func fileUsesConcurrency(f *ast.File) bool {
	for _, imp := range f.Imports {
		if concurrencyImports[strings.Trim(imp.Path.Value, `"`)] {
			return true
		}
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if found {
			return false
		}
		switch x := n.(type) {
		case *ast.GoStmt, *ast.SelectStmt, *ast.SendStmt, *ast.ChanType:
			found = true
		case *ast.UnaryExpr:
			if x.Op == token.ARROW {
				found = true
			}
		}
		return !found
	})
	return found
}

// ---------------------------------------------------------------- affected

// fallbackRules: a changed path matching any of these makes the run test
// everything. Patterns use path.Match per segment ("**" = any depth).
var fallbackRules = []string{
	"go.mod",
	"go.sum",
	".github/workflows/ci.yml",
	".github/actions/**",
	"scripts/ci/**",
	"scripts/omit-tag-check.sh",
	"testdata/**",
	"**/migrations/**",
	"**/pgmigrations/**",
	"Makefile",
	"Makefile.private",
}

// unreadRules: paths no Go test reads, so a change to them alone affects no
// package (the same list ci.yml's paths-ignore uses).
var unreadRules = []string{
	"docs/handovers/**",
	"docs/audits/**",
	"docs/decisions/**",
	"docs/strategy/**",
	"docs/demos/**",
	"docs/plans/*.md",
	"marketing/**",
	"design/**",
	"PROGRESS.md",
	"CLAUDE.md",
	"AGENTS.md",
	"CHANGELOG.md",
	"kickstart-prompt.md",
	"superbased-final-spec-v2.md",
	".github/workflows/*.yml",
	".github/dependabot.yml",
}

// alwaysRun are test packages every affected run includes: tests/invariant
// reads product source across the tree, so the import graph cannot say
// which change touches it.
var alwaysRun = []string{"tests/invariant"}

// matchRule reports whether rel (slash-separated) matches pattern, where a
// "**" segment matches zero or more path segments.
func matchRule(pattern, rel string) bool {
	return matchSegs(strings.Split(pattern, "/"), strings.Split(rel, "/"))
}

func matchSegs(p, s []string) bool {
	for len(p) > 0 {
		if p[0] == "**" {
			for i := 0; i <= len(s); i++ {
				if matchSegs(p[1:], s[i:]) {
					return true
				}
			}
			return false
		}
		if len(s) == 0 {
			return false
		}
		if ok, _ := path.Match(p[0], s[0]); !ok {
			return false
		}
		p, s = p[1:], s[1:]
	}
	return len(s) == 0
}

func anyRule(rules []string, rel string) bool {
	for _, r := range rules {
		if matchRule(r, rel) {
			return true
		}
	}
	return false
}

// classify maps the changed files to the set of changed package dirs. all is
// true when the run must test everything.
func classify(changed []string, pkgDirs map[string]bool) (dirs map[string]bool, all bool, why string) {
	dirs = map[string]bool{}
	for _, c := range changed {
		if anyRule(fallbackRules, c) {
			return nil, true, "shared input changed: " + c
		}
		if anyRule(unreadRules, c) {
			continue
		}
		d := path.Dir(c)
		for {
			if pkgDirs[d] {
				dirs[d] = true
				break
			}
			if d == "." || d == "/" {
				return nil, true, "file outside every package and not on the unread list: " + c
			}
			d = path.Dir(d)
		}
	}
	return dirs, false, ""
}

func affected(root, module, base, head string) ([]string, error) {
	if base == "" || strings.Trim(base, "0") == "" {
		fmt.Fprintln(os.Stderr, "cipkgs: no base commit; testing everything")
		return []string{"ALL"}, nil
	}
	diff := exec.Command("git", "diff", "--name-only", base, head)
	diff.Dir = root
	out, err := diff.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cipkgs: git diff %s %s failed (%v); testing everything\n", base, head, err)
		return []string{"ALL"}, nil //nolint:nilerr // fail toward more testing
	}
	var changed []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" {
			changed = append(changed, l)
		}
	}
	pkgs, err := listPackages(root)
	if err != nil {
		return nil, err
	}
	pkgDirs := map[string]bool{}
	for _, p := range pkgs {
		pkgDirs[p.rel] = true
	}
	dirs, all, why := classify(changed, pkgDirs)
	if all {
		fmt.Fprintln(os.Stderr, "cipkgs: "+why+"; testing everything")
		return []string{"ALL"}, nil
	}
	// Reverse dependencies through the test graph.
	changedImports := map[string]bool{}
	for d := range dirs {
		changedImports[importPath(module, d)] = true
	}
	ls, err := goListJSON(root, "-test", "-deps=false", "./...")
	if err != nil {
		return nil, err
	}
	hit := map[string]bool{}
	for _, l := range ls {
		owner := l.ImportPath
		if l.ForTest != "" {
			owner = l.ForTest
		}
		if strings.HasSuffix(owner, ".test") {
			continue
		}
		if changedImports[owner] {
			hit[owner] = true
			continue
		}
		for _, d := range l.Deps {
			if changedImports[d] {
				hit[owner] = true
				break
			}
		}
	}
	for _, a := range alwaysRun {
		hit[importPath(module, a)] = true
	}
	res := make([]string, 0, len(hit))
	for h := range hit {
		res = append(res, relPath(module, h))
	}
	sort.Strings(res)
	fmt.Fprintf(os.Stderr, "cipkgs: %d changed file(s) in %d package(s) affect %d package(s)\n", len(changed), len(dirs), len(res))
	return res, nil
}

func importPath(module, rel string) string {
	if rel == "." {
		return module
	}
	return module + "/" + rel
}

func relPath(module, imp string) string {
	if imp == module {
		return "."
	}
	return strings.TrimPrefix(imp, module+"/")
}
