package toolresolve

import (
	"io/fs"
	"reflect"
	"testing"
)

// TestChildInterpreterPath pins which node a `#!/usr/bin/env node` shim runs on
// under the launcher's composed PATH: the shim's own dir first, then the
// login-only dirs, then the process PATH. The node-1 shape (2026-10-04): the
// shim in ~/.local/bin, no node beside it, /usr/bin/node v18 on the PATH.
func TestChildInterpreterPath(t *testing.T) {
	shim := p("/home/u/.local/bin/gemini")
	cases := []struct {
		name  string
		files map[string]fs.FileMode
		login []string
		want  string
	}{
		{"node-1 shape: falls through to /usr/bin/node", map[string]fs.FileMode{
			shim: exeMode, p("/usr/bin/node"): exeMode,
		}, nil, p("/usr/bin/node")},
		{"a node beside the shim wins", map[string]fs.FileMode{
			shim: exeMode, p("/home/u/.local/bin/node"): exeMode, p("/usr/bin/node"): exeMode,
		}, nil, p("/home/u/.local/bin/node")},
		{"a login-only node beats the process PATH", map[string]fs.FileMode{
			shim: exeMode, p("/home/u/.nvm/versions/node/v24/bin/node"): exeMode, p("/usr/bin/node"): exeMode,
		}, []string{p("/home/u/.nvm/versions/node/v24/bin"), p("/usr/bin")}, p("/home/u/.nvm/versions/node/v24/bin/node")},
		{"a non-executable node is skipped", map[string]fs.FileMode{
			shim: exeMode, p("/home/u/.local/bin/node"): regMode, p("/usr/bin/node"): exeMode,
		}, nil, p("/usr/bin/node")},
		{"none resolves", map[string]fs.FileMode{shim: exeMode}, nil, ""},
	}
	for _, tc := range cases {
		fsys := fakeFS{files: tc.files}
		env := fsys.env(Env{GOOS: "linux", ProcessPath: []string{p("/home/u/.local/bin"), p("/usr/bin")}})
		if tc.login != nil {
			login := tc.login
			env.LoginPath = func() ([]string, error) { return login, nil }
		}
		res := Resolution{Verdict: VerdictOK, Bin: shim, Interpreter: "node"}
		if got := ChildInterpreterPath(res, env); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := ChildInterpreterPath(Resolution{Bin: shim}, Env{}); got != "" {
		t.Errorf("no interpreter: got %q", got)
	}
}

// TestChildPathDirsOrder pins the composition order and dedup.
func TestChildPathDirsOrder(t *testing.T) {
	env := Env{
		GOOS:        "linux",
		ProcessPath: []string{p("/usr/bin"), "relative", p("/bin")},
		LoginPath:   func() ([]string, error) { return []string{p("/opt/node/bin"), p("/usr/bin")}, nil },
	}
	got := ChildPathDirs(env, p("/home/u/.local/bin"))
	want := []string{p("/home/u/.local/bin"), p("/opt/node/bin"), p("/usr/bin"), p("/bin")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ChildPathDirs = %v, want %v", got, want)
	}
}
