package hookdisable

import (
	"errors"
	"go/build"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// bom is a UTF-8 byte order mark, spelled as an escape so the source stays BOM-free.
const bom = "\ufeff"

func reader(files map[string]string) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		if s, ok := files[p]; ok {
			return []byte(s), nil
		}
		return nil, errors.New("absent")
	}
}

func TestDetect(t *testing.T) {
	home := "/h"
	claude := filepath.Join(home, ".claude", "settings.json")
	qwen := filepath.Join(home, ".qwen", "settings.json")
	gemini := filepath.Join(home, ".gemini", "settings.json")
	factory := filepath.Join(home, ".factory", "settings.json")
	managed := "/etc/claude-code/managed-settings.json"
	cases := []struct {
		name  string
		files map[string]string
		goos  string
		want  []string
	}{
		{"nothing installed", nil, "linux", nil},
		{"claude off", map[string]string{claude: `{"disableAllHooks":true}`}, "linux", []string{"claude.disableAllHooks"}},
		{"claude explicitly on", map[string]string{claude: `{"disableAllHooks":false}`}, "linux", nil},
		{"claude non-bool is not a finding", map[string]string{claude: `{"disableAllHooks":"yes"}`}, "linux", nil},
		{"claude malformed is not a finding", map[string]string{claude: `{"disableAllHooks":tru`}, "linux", nil},
		{"claude BOM", map[string]string{claude: bom + "{\"disableAllHooks\":true}"}, "linux", []string{"claude.disableAllHooks"}},
		{"managed off", map[string]string{managed: `{"disableAllHooks":true}`}, "linux", []string{"claude.managed.disableAllHooks"}},
		{"managed only", map[string]string{managed: `{"allowManagedHooksOnly":true}`}, "linux", []string{"claude.managed.allowManagedHooksOnly"}},
		{"managed skipped without GOOS", map[string]string{managed: `{"disableAllHooks":true}`}, "", nil},
		{"qwen off", map[string]string{qwen: `{"disableAllHooks":true}`}, "linux", []string{"qwen.disableAllHooks"}},
		{"gemini off", map[string]string{gemini: `{"hooksConfig":{"enabled":false}}`}, "linux", []string{"gemini.hooksConfig.enabled"}},
		{"gemini on", map[string]string{gemini: `{"hooksConfig":{"enabled":true}}`}, "linux", nil},
		{"gemini key absent", map[string]string{gemini: `{"hooksConfig":{}}`}, "linux", nil},
		{"copilot-cli off", map[string]string{filepath.Join(home, ".copilot", "settings.json"): `{"disableAllHooks":true}`}, "linux", []string{"copilot-cli.disableAllHooks"}},
		{"factory off", map[string]string{factory: `{"hooksDisabled":true}`}, "linux", []string{"factory.hooksDisabled"}},
		{
			"several",
			map[string]string{claude: `{"disableAllHooks":true}`, factory: `{"hooksDisabled":true}`},
			"linux",
			[]string{"claude.disableAllHooks", "factory.hooksDisabled"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Detect(Options{Home: home, GOOS: tc.goos, ReadFile: reader(tc.files)})
			var ids []string
			for _, f := range got {
				ids = append(ids, f.SwitchID)
			}
			if !reflect.DeepEqual(ids, tc.want) {
				t.Fatalf("Detect = %v, want %v", ids, tc.want)
			}
		})
	}
}

func TestDetectClaudeSilencesTheReplayHosts(t *testing.T) {
	claude := filepath.Join("/h", ".claude", "settings.json")
	fs := Detect(Options{Home: "/h", ReadFile: reader(map[string]string{claude: `{"disableAllHooks":true}`})})
	got := AffectedTools(fs)
	want := []string{"claude-code", "copilot", "copilot-cli", "cursor", "deepseek", "devin", "grok"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AffectedTools = %v, want %v (T-046: the six audited hosts plus live-grounded cursor-agent)", got, want)
	}
}

func TestDetectNilReader(t *testing.T) {
	if got := Detect(Options{Home: "/h"}); got != nil {
		t.Fatalf("nil reader: %v", got)
	}
}

func TestTrackerTransitions(t *testing.T) {
	a := Finding{SwitchID: "a", Path: "/p"}
	b := Finding{SwitchID: "b", Path: "/p"}
	var tr Tracker
	steps := []struct {
		in   []Finding
		want []string
	}{
		{[]Finding{a}, []string{"a"}},
		{[]Finding{a}, nil},
		{[]Finding{a, b}, []string{"b"}},
		{nil, nil},
		{[]Finding{a}, []string{"a"}},
	}
	for i, s := range steps {
		var ids []string
		for _, f := range tr.Turned(s.in) {
			ids = append(ids, f.SwitchID)
		}
		if !reflect.DeepEqual(ids, s.want) {
			t.Fatalf("step %d: Turned = %v, want %v", i, ids, s.want)
		}
	}
}

func TestSwitchTableRowsAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range Switches {
		if seen[s.ID] {
			t.Errorf("duplicate switch id %s", s.ID)
		}
		seen[s.ID] = true
		if len(s.Tools) == 0 || s.Key == "" || s.Effect == "" || s.Grounding == "" {
			t.Errorf("%s: incomplete row", s.ID)
		}
		switch s.Scope {
		case ScopeUser:
			if s.HomeRel == "" {
				t.Errorf("%s: user row without HomeRel", s.ID)
			}
		case ScopeManaged:
			if len(s.Abs) == 0 {
				t.Errorf("%s: managed row without Abs", s.ID)
			}
		default:
			t.Errorf("%s: scope %q", s.ID, s.Scope)
		}
	}
}

// TestImportsArePure pins the package free of database, network and file
// watching imports (CLAUDE.md module boundaries).
func TestImportsArePure(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range pkg.Imports {
		for _, bad := range []string{"database/sql", "net/http", "fsnotify", "os/exec", "superbased-observer/internal/store"} {
			if strings.Contains(imp, bad) {
				t.Errorf("hookdisable imports %s", imp)
			}
		}
		if imp == "os" {
			t.Errorf("hookdisable imports os: the file read must be injected")
		}
	}
}
