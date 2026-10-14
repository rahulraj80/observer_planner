package installshape

import (
	"reflect"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// fs is a fake Resolve: path -> resolved path. A key that is absent does not
// exist.
type fs map[string]string

func (f fs) resolve(p string) (string, bool) {
	r, ok := f[p]
	return r, ok
}

const (
	npmPrefix  = "/home/u/.npm-global"
	npmShimJS  = npmPrefix + "/lib/node_modules/@superbased/observer/bin/observer.js"
	npmHoisted = npmPrefix + "/lib/node_modules/@superbased/observer-linux-x64/bin/observer"
	npmNested  = npmPrefix + "/lib/node_modules/@superbased/observer/node_modules/@superbased/observer-linux-x64/bin/observer"
	venv       = "/home/u/venv"
	pipBin     = venv + "/lib/python3.12/site-packages/observer/_bin/observer"
)

// TestDecideTable is one case per row of the registration table (U1), plus
// the no-launcher fallbacks.
func TestDecideTable(t *testing.T) {
	npmFS := fs{
		npmPrefix + "/bin/observer": npmShimJS,
	}
	cases := []struct {
		name    string
		in      Input
		channel Channel
		command string
		rule    string
	}{
		{
			name:    "npm hoisted (1.36 layout), launcher on PATH",
			in:      Input{GOOS: "linux", Binary: npmHoisted, PathCandidates: []string{npmPrefix + "/bin/observer"}, Resolve: npmFS.resolve},
			channel: ChannelNPM, command: npmPrefix + "/bin/observer", rule: "npm",
		},
		{
			name:    "npm nested (1.34 layout) maps to the same launcher",
			in:      Input{GOOS: "linux", Binary: npmNested, PathCandidates: []string{npmPrefix + "/bin/observer"}, Resolve: npmFS.resolve},
			channel: ChannelNPM, command: npmPrefix + "/bin/observer", rule: "npm",
		},
		{
			name:    "npm, launcher not on PATH: the prefix bin fallback",
			in:      Input{GOOS: "darwin", Binary: npmHoisted, Resolve: npmFS.resolve},
			channel: ChannelNPM, command: npmPrefix + "/bin/observer", rule: "npm",
		},
		{
			name: "npm, a PATH observer from ANOTHER install is never taken",
			in: Input{
				GOOS: "linux", Binary: npmHoisted, PathCandidates: []string{"/home/u/.local/bin/observer"},
				Resolve: fs{"/home/u/.local/bin/observer": "/home/u/.local/bin/observer"}.resolve,
			},
			channel: ChannelNPM, command: npmHoisted, rule: "npm-no-launcher",
		},
		{
			name: "npm local install: node_modules/.bin",
			in: Input{
				GOOS: "linux", Binary: "/p/node_modules/@superbased/observer-linux-arm64/bin/observer",
				Resolve: fs{"/p/node_modules/.bin/observer": "/p/node_modules/@superbased/observer/bin/observer.js"}.resolve,
			},
			channel: ChannelNPM, command: "/p/node_modules/.bin/observer", rule: "npm",
		},
		{
			name: "npm on Windows keeps the real path",
			in: Input{
				GOOS: "windows", Binary: `C:\Users\u\AppData\Roaming\npm\node_modules\@superbased\observer-win32-x64\bin\observer.exe`,
				PathCandidates: []string{`C:\Users\u\AppData\Roaming\npm\observer.cmd`}, Resolve: fs{}.resolve,
			},
			channel: ChannelNPM, command: `C:\Users\u\AppData\Roaming\npm\node_modules\@superbased\observer-win32-x64\bin\observer.exe`, rule: "npm-windows",
		},
		{
			name: "pip venv, console script on PATH",
			in: Input{
				GOOS: "linux", Binary: pipBin, PathCandidates: []string{"/usr/local/bin/observer", venv + "/bin/observer"},
				Resolve: fs{"/usr/local/bin/observer": "/usr/local/bin/observer", venv + "/bin/observer": venv + "/bin/observer"}.resolve,
			},
			channel: ChannelPip, command: venv + "/bin/observer", rule: "pip",
		},
		{
			name: "pipx: the PATH symlink into the venv wins",
			in: Input{
				GOOS: "linux", Binary: "/home/u/.local/share/pipx/venvs/superbased-observer/lib/python3.12/site-packages/observer/_bin/observer",
				PathCandidates: []string{"/home/u/.local/bin/observer"},
				Resolve:        fs{"/home/u/.local/bin/observer": "/home/u/.local/share/pipx/venvs/superbased-observer/bin/observer"}.resolve,
			},
			channel: ChannelPip, command: "/home/u/.local/bin/observer", rule: "pip",
		},
		{
			name:    "pip, console script missing: real path",
			in:      Input{GOOS: "linux", Binary: pipBin, Resolve: fs{}.resolve},
			channel: ChannelPip, command: pipBin, rule: "pip-no-launcher",
		},
		{
			name: "pip on Windows: Scripts\\observer.exe",
			in: Input{
				GOOS: "windows", Binary: `C:\v\Lib\site-packages\observer\_bin\observer.exe`,
				Resolve: fs{`C:\v\Scripts\observer.exe`: `C:\v\Scripts\observer.exe`}.resolve,
			},
			channel: ChannelPip, command: `C:\v\Scripts\observer.exe`, rule: "pip",
		},
		{
			name: "archive install keeps the real path",
			in: Input{
				GOOS: "linux", Binary: "/home/u/.local/bin/observer", PathCandidates: []string{"/home/u/.local/bin/observer"},
				Resolve: fs{"/home/u/.local/bin/observer": "/home/u/.local/bin/observer"}.resolve,
			},
			channel: ChannelOther, command: "/home/u/.local/bin/observer", rule: "other",
		},
		{
			name:    "nil Resolve keeps the real path",
			in:      Input{GOOS: "linux", Binary: npmHoisted},
			channel: ChannelNPM, command: npmHoisted, rule: "npm-no-launcher",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := Decide(c.in)
			if d.Channel != c.channel || d.Command != c.command || d.Rule != c.rule {
				t.Fatalf("Decide = {%s %q %s}, want {%s %q %s}", d.Channel, d.Command, d.Rule, c.channel, c.command, c.rule)
			}
			if d.Reason == "" {
				t.Fatal("every decision names its reason")
			}
		})
	}
}

// TestRulesOrder pins the table order: the Windows npm row must precede the
// generic npm row, and the catch-all comes last.
func TestRulesOrder(t *testing.T) {
	want := []string{"npm-windows", "npm", "pip", "other"}
	if got := Rules(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Rules() = %v, want %v", got, want)
	}
}

// TestNoForbiddenImports pins purity (CLAUDE.md module boundaries).
func TestNoForbiddenImports(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		Forbidden: []string{"database/sql", "net/http", "os", "os/exec", "path/filepath", "github.com/fsnotify/fsnotify"},
		Reason:    "internal/installshape must stay pure",
	})
}
