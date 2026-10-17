package toolresolve

import (
	"io/fs"
	"testing"
)

// TestNPMPackageRoot pins how a resolved shim maps to its npm package root.
func TestNPMPackageRoot(t *testing.T) {
	links := map[string]string{
		p("/home/u/.local/bin/gemini"): p("/home/u/.local/lib/node_modules/@google/gemini-cli/bundle/gemini.js"),
		p("/home/u/.local/bin/codex"):  p("/home/u/.local/lib/node_modules/@openai/codex/bin/codex.js"),
		p("/usr/bin/freebuff"):         p("/usr/lib/node_modules/freebuff/index.js"),
	}
	env := Env{EvalSymlinks: func(path string) (string, error) {
		if r, ok := links[path]; ok {
			return r, nil
		}
		return "", fs.ErrNotExist
	}}
	cases := []struct {
		name, bin, want string
	}{
		{"scoped package through a symlinked shim (node 1)", p("/home/u/.local/bin/gemini"), p("/home/u/.local/lib/node_modules/@google/gemini-cli")},
		{"scoped package, bin/ entry", p("/home/u/.local/bin/codex"), p("/home/u/.local/lib/node_modules/@openai/codex")},
		{"unscoped package", p("/usr/bin/freebuff"), p("/usr/lib/node_modules/freebuff")},
		{"a file directly in the package dir", p("/x/node_modules/pkg/cli.js"), p("/x/node_modules/pkg")},
		{"the deepest node_modules wins", p("/x/node_modules/a/node_modules/@s/b/run.js"), p("/x/node_modules/a/node_modules/@s/b")},
		{"not in node_modules: a native binary", p("/usr/local/bin/claude"), ""},
		{"the package dir itself is not a binary", p("/x/node_modules/pkg"), ""},
		{"a scope with no package", p("/x/node_modules/@scope"), ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		if got := NPMPackageRoot(env, tc.bin); got != tc.want {
			t.Errorf("%s: NPMPackageRoot(%q) = %q, want %q", tc.name, tc.bin, got, tc.want)
		}
	}
}
