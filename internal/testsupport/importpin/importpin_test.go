package importpin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recorder is a testing.TB that records failures instead of failing, so
// the guard itself can be tested for both outcomes.
type recorder struct {
	testing.TB
	errs []string
}

func (r *recorder) Helper() {}
func (r *recorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

func writePkg(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCheck(t *testing.T) {
	const mod = "github.com/marmutapp/superbased-observer/internal/"
	cases := []struct {
		name    string
		files   map[string]string
		spec    Spec
		wantErr string // substring of the first error; "" means pass
	}{
		{
			"clean package passes",
			map[string]string{"a.go": "package p\nimport \"sort\"\n"},
			Spec{Forbidden: []string{"net/http"}},
			"",
		},
		{
			"exact forbidden import fails",
			map[string]string{"a.go": "package p\nimport \"net/http\"\n"},
			Spec{Forbidden: []string{"net/http"}},
			`import "net/http": forbidden`,
		},
		{
			"raw-string import literal cannot bypass",
			map[string]string{"a.go": "package p\nimport _ `net/http`\n"},
			Spec{Forbidden: []string{"net/http"}},
			`import "net/http": forbidden`,
		},
		{
			"exact rule ignores a subpackage",
			map[string]string{"a.go": "package p\nimport \"" + mod + "store/x\"\n"},
			Spec{Forbidden: []string{mod + "store"}},
			"",
		},
		{
			"tree rule catches a subpackage",
			map[string]string{"a.go": "package p\nimport \"" + mod + "store/x\"\n"},
			Spec{ForbiddenTrees: []string{mod + "store"}},
			"forbidden (under",
		},
		{
			"tree rule ignores a sibling with a shared prefix",
			map[string]string{"a.go": "package p\nimport \"" + mod + "storex\"\n"},
			Spec{ForbiddenTrees: []string{mod + "store"}},
			"",
		},
		{
			"prefix rule is a raw prefix",
			map[string]string{"a.go": "package p\nimport \"" + mod + "storex\"\n"},
			Spec{ForbiddenPrefixes: []string{mod + "store"}},
			"forbidden (prefix",
		},
		{
			"except exempts",
			map[string]string{"a.go": "package p\nimport \"" + mod + "orgserver/ok\"\n"},
			Spec{ForbiddenTrees: []string{mod + "orgserver"}, Except: []string{mod + "orgserver/ok"}},
			"",
		},
		{
			"allow-list rejects anything else",
			map[string]string{"a.go": "package p\nimport \"strings\"\n"},
			Spec{AllowOnly: true, Allowed: []string{"sort"}},
			"not in the allow-list",
		},
		{
			"stdlib-only rejects a module import",
			map[string]string{"a.go": "package p\nimport \"github.com/x/y\"\n"},
			Spec{StdlibOnly: true},
			"not standard library",
		},
		{
			"stdlib-only admits an allowed tree",
			map[string]string{"a.go": "package p\nimport \"" + mod + "models\"\n"},
			Spec{StdlibOnly: true, AllowedTrees: []string{mod + "models"}},
			"",
		},
		{
			"tests are skipped by default",
			map[string]string{"a.go": "package p\n", "a_test.go": "package p\nimport \"net/http\"\n"},
			Spec{Forbidden: []string{"net/http"}},
			"",
		},
		{
			"IncludeTests scans tests",
			map[string]string{"a.go": "package p\n", "a_test.go": "package p\nimport \"net/http\"\n"},
			Spec{Forbidden: []string{"net/http"}, IncludeTests: true},
			"forbidden",
		},
		{
			"subdirectories need Recursive",
			map[string]string{"a.go": "package p\n", "sub/b.go": "package sub\nimport \"net/http\"\n"},
			Spec{Forbidden: []string{"net/http"}},
			"",
		},
		{
			"Recursive walks subdirectories",
			map[string]string{"a.go": "package p\n", "sub/b.go": "package sub\nimport \"net/http\"\n"},
			Spec{Forbidden: []string{"net/http"}, Recursive: true},
			"sub/b.go",
		},
		{
			"Recursive skips testdata fixtures",
			map[string]string{"a.go": "package p\n", "testdata/b.go": "package b\nimport \"net/http\"\n"},
			Spec{Forbidden: []string{"net/http"}, Recursive: true},
			"",
		},
		{
			"custom rule",
			map[string]string{"a.go": "package p\nimport \"os\"\n"},
			Spec{Custom: func(_, p string) string {
				if p == "os" {
					return "no os"
				}
				return ""
			}},
			"no os",
		},
		{
			"an empty scan fails",
			map[string]string{"a_test.go": "package p\n"},
			Spec{Forbidden: []string{"net/http"}},
			"vacuous",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writePkg(t, tc.files)
			tc.spec.Dirs = []string{dir}
			r := &recorder{TB: t}
			Check(r, tc.spec)
			if tc.wantErr == "" {
				if len(r.errs) != 0 {
					t.Fatalf("want pass, got %q", r.errs)
				}
				return
			}
			if len(r.errs) == 0 || !strings.Contains(r.errs[0], tc.wantErr) {
				t.Fatalf("want an error containing %q, got %q", tc.wantErr, r.errs)
			}
		})
	}
}

func TestCheckMissingDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	r := &recorder{TB: t}
	Check(r, Spec{Dirs: []string{missing}, Forbidden: []string{"net/http"}})
	if len(r.errs) == 0 {
		t.Fatal("a missing directory must fail")
	}
	r = &recorder{TB: t}
	present := writePkg(t, map[string]string{"a.go": "package p\n"})
	Check(r, Spec{Dirs: []string{missing, present}, SkipMissingDirs: true, Forbidden: []string{"net/http"}})
	if len(r.errs) != 0 {
		t.Fatalf("SkipMissingDirs must skip, got %q", r.errs)
	}
}

func TestCheckFiles(t *testing.T) {
	dir := writePkg(t, map[string]string{"a.go": "package p\nimport \"net/http\"\n", "b.go": "package p\n"})
	r := &recorder{TB: t}
	Check(r, Spec{Files: []string{filepath.Join(dir, "b.go")}, Forbidden: []string{"net/http"}})
	if len(r.errs) != 0 {
		t.Fatalf("only the pinned file is scanned, got %q", r.errs)
	}
	r = &recorder{TB: t}
	Check(r, Spec{Files: []string{filepath.Join(dir, "a.go")}, Forbidden: []string{"net/http"}})
	if len(r.errs) != 1 {
		t.Fatalf("the pinned file's import must fail, got %q", r.errs)
	}
	r = &recorder{TB: t}
	Check(r, Spec{Files: []string{filepath.Join(dir, "gone.go")}, Forbidden: []string{"net/http"}})
	if len(r.errs) != 1 || !strings.Contains(r.errs[0], "pinned file") {
		t.Fatalf("a missing pinned file must fail, got %q", r.errs)
	}
}

func TestReasonIsAppended(t *testing.T) {
	dir := writePkg(t, map[string]string{"a.go": "package p\nimport \"net/http\"\n"})
	r := &recorder{TB: t}
	Check(r, Spec{Dirs: []string{dir}, Forbidden: []string{"net/http"}, Reason: "pkg p is pure"})
	if len(r.errs) != 1 || !strings.Contains(r.errs[0], "(pkg p is pure)") {
		t.Fatalf("got %q", r.errs)
	}
}
