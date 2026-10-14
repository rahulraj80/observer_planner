package importpin

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Spec is one package's import boundary. Every rule is evaluated against
// each import of every scanned file; an import that trips any rule is
// reported. The zero Spec scans the test's working directory (the package
// directory under go test) and forbids nothing, which is useless, so a
// caller always sets at least one rule.
type Spec struct {
	// Dirs are the directories scanned, relative to the working directory.
	// Empty means ".".
	Dirs []string
	// Files, when set, replaces the directory scan with exactly these files
	// (relative to the working directory); a missing file fails.
	Files []string
	// Recursive walks every subdirectory of each Dir as well, except
	// testdata trees.
	Recursive bool
	// SkipMissingDirs skips a Dir that does not exist instead of failing.
	// Use it only for a list of packages that are allowed to not exist yet.
	SkipMissingDirs bool
	// IncludeTests scans _test.go files too. Off by default: tests may use
	// broader infrastructure than the code they test.
	IncludeTests bool

	// Forbidden fails an import equal to an entry.
	Forbidden []string
	// ForbiddenTrees fails an import equal to an entry or below it
	// (entry + "/...").
	ForbiddenTrees []string
	// ForbiddenPrefixes fails an import that starts with an entry, as a raw
	// string prefix.
	ForbiddenPrefixes []string
	// Except exempts exact import paths from the three Forbidden rules.
	Except []string
	// ExceptTrees exempts an import path and everything below it from the
	// three Forbidden rules.
	ExceptTrees []string

	// AllowOnly turns Allowed/AllowedTrees into a closed allow-list: any
	// import not matched by them fails.
	AllowOnly bool
	// StdlibOnly fails any non-standard-library import (first path element
	// contains a dot) unless Allowed/AllowedTrees match it.
	StdlibOnly bool
	// Allowed are exact import paths admitted by AllowOnly / StdlibOnly.
	Allowed []string
	// AllowedTrees admit an import path and everything below it.
	AllowedTrees []string

	// Custom, when set, is consulted for every import; a non-empty return is
	// the failure reason.
	Custom func(file, importPath string) string

	// Reason is appended to every failure message, naming the boundary.
	Reason string
}

// Check scans the Spec's directories and reports every import that breaks
// the boundary through t.Errorf. It also fails when it scanned no file, so
// a moved or renamed package can never turn its pin into a silent pass.
func Check(t testing.TB, s Spec) {
	t.Helper()
	files, ok := collect(t, s)
	if !ok {
		return
	}
	if len(files) == 0 {
		t.Errorf("importpin: no .go files scanned in %v: the pin would be vacuous", dirsOf(s))
		return
	}
	fset := token.NewFileSet()
	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("importpin: parse %s: %v", path, err)
			continue
		}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Errorf("%s: unparseable import literal %s: %v", path, imp.Path.Value, err)
				continue
			}
			if why := Violation(s, path, p); why != "" {
				msg := why
				if s.Reason != "" {
					msg += " (" + s.Reason + ")"
				}
				t.Errorf("%s: import %q: %s", filepath.ToSlash(path), p, msg)
			}
		}
	}
}

// Violation reports why importPath, imported by file, breaks the Spec, or
// "" when it does not. Exported so a package with a bespoke companion test
// can reuse the exact rule set.
func Violation(s Spec, file, importPath string) string {
	if !excepted(s, importPath) {
		for _, bad := range s.Forbidden {
			if importPath == bad {
				return "forbidden"
			}
		}
		for _, bad := range s.ForbiddenTrees {
			if under(importPath, bad) {
				return "forbidden (under " + bad + ")"
			}
		}
		for _, bad := range s.ForbiddenPrefixes {
			if strings.HasPrefix(importPath, bad) {
				return "forbidden (prefix " + bad + ")"
			}
		}
	}
	if s.AllowOnly && !allowed(s, importPath) {
		return "not in the allow-list " + strings.Join(sortedCopy(append(append([]string{}, s.Allowed...), s.AllowedTrees...)), ", ")
	}
	if s.StdlibOnly && !isStdlib(importPath) && !allowed(s, importPath) {
		return "not standard library and not allow-listed"
	}
	if s.Custom != nil {
		if why := s.Custom(file, importPath); why != "" {
			return why
		}
	}
	return ""
}

func excepted(s Spec, p string) bool {
	for _, e := range s.Except {
		if p == e {
			return true
		}
	}
	for _, e := range s.ExceptTrees {
		if under(p, e) {
			return true
		}
	}
	return false
}

func allowed(s Spec, p string) bool {
	for _, a := range s.Allowed {
		if p == a {
			return true
		}
	}
	for _, a := range s.AllowedTrees {
		if under(p, a) {
			return true
		}
	}
	return false
}

func under(p, root string) bool {
	return p == root || strings.HasPrefix(p, root+"/")
}

func isStdlib(p string) bool {
	first, _, _ := strings.Cut(p, "/")
	return !strings.Contains(first, ".")
}

func sortedCopy(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}

func dirsOf(s Spec) []string {
	if len(s.Dirs) == 0 {
		return []string{"."}
	}
	return s.Dirs
}

// collect lists the .go files the Spec scans. ok is false when a fatal
// condition was already reported.
func collect(t testing.TB, s Spec) (files []string, ok bool) {
	t.Helper()
	keep := func(name string) bool {
		if !strings.HasSuffix(name, ".go") {
			return false
		}
		return s.IncludeTests || !strings.HasSuffix(name, "_test.go")
	}
	if len(s.Files) > 0 {
		for _, f := range s.Files {
			if _, err := os.Stat(f); err != nil {
				t.Errorf("importpin: pinned file %s: %v", f, err)
				return nil, false
			}
		}
		return append([]string{}, s.Files...), true
	}
	for _, dir := range dirsOf(s) {
		info, err := os.Stat(dir)
		if err != nil {
			if s.SkipMissingDirs && os.IsNotExist(err) {
				continue
			}
			t.Errorf("importpin: %s: %v", dir, err)
			return nil, false
		}
		if !info.IsDir() {
			t.Errorf("importpin: %s is not a directory", dir)
			return nil, false
		}
		if s.Recursive {
			err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() && d.Name() == "testdata" && path != dir {
					// Fixture trees are not package code (the go tool
					// ignores them too).
					return filepath.SkipDir
				}
				if !d.IsDir() && keep(d.Name()) {
					files = append(files, path)
				}
				return nil
			})
			if err != nil {
				t.Errorf("importpin: walk %s: %v", dir, err)
				return nil, false
			}
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Errorf("importpin: read %s: %v", dir, err)
			return nil, false
		}
		for _, e := range entries {
			if !e.IsDir() && keep(e.Name()) {
				files = append(files, filepath.Join(dir, e.Name()))
			}
		}
	}
	return files, true
}
