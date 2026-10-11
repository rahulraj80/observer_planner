package toolinstall

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

// TestEnginesFloor pins the lower bound read from an npm engines range: the
// minimum over its || alternatives; no floor at all is ok=false (the caller
// keeps the registry floor rather than guess).
func TestEnginesFloor(t *testing.T) {
	cases := []struct {
		rng  string
		want string
		ok   bool
	}{
		{">=20", "20.0.0", true},
		{">=22.0.0", "22.0.0", true},
		{">=22.19.0", "22.19.0", true},
		{">=24.16.0<25||>=26.1.0", "24.16.0", true}, // openclaw 2026.9.8
		{">=24.16.0 <25 || >=26.1.0", "24.16.0", true},
		{"^20.0.0 || >=22", "20.0.0", true},
		{">=22 || ^20.11", "20.11.0", true},
		{"20.x", "20.0.0", true},
		{"~18.2", "18.2.0", true},
		{">= 18", "18.0.0", true},
		{"v20.1.2", "20.1.2", true},
		{"1.2.3 - 2.3.4", "1.2.3", true},
		{"*", "", false},
		{"x", "", false},
		{"", "", false},
		{"latest", "", false},
		{"<20", "", false},
		{">=20 || <16", "", false}, // one alternative has no floor
		// Review M3-18: a strict > is exact, not >=.
		{">20", "21.0.0", true},
		{">20.1", "20.2.0", true},
		{">20.1.2", "20.1.3", true},
		{"<25 >=20", "20.0.0", true}, // the floor need not come first
		{">=20 garbage", "", false},
	}
	for _, tc := range cases {
		got, ok := EnginesFloor(tc.rng)
		if ok != tc.ok || got != tc.want {
			t.Errorf("EnginesFloor(%q) = %q,%v want %q,%v", tc.rng, got, ok, tc.want, tc.ok)
		}
	}
}

// TestInstalledFloor: an installed package.json's engines.node, present,
// absent, unparseable, or a range with a floor.
func TestInstalledFloor(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
		ok   bool
	}{
		{"present (gemini-cli 0.62.0)", `{"name":"@google/gemini-cli","version":"0.62.0","engines":{"node":">=20"}}`, "20.0.0", true},
		{"range with a floor", `{"engines":{"node":">=24.16.0<25||>=26.1.0"}}`, "24.16.0", true},
		{"absent: no engines", `{"name":"opencode-ai","version":"1.18.34"}`, "", false},
		{"absent: engines without node", `{"engines":{"npm":">=9"}}`, "", false},
		{"absent: empty file", ``, "", false},
		{"unparseable JSON", `{"engines": {"node": ">=20"`, "", false},
		{"unparseable: engines is an array (old packages)", `{"engines":["node >= 0.8"]}`, "", false},
		{"unparseable: node is not a string", `{"engines":{"node":20}}`, "", false},
		{"no floor: any version", `{"engines":{"node":"*"}}`, "", false},
	}
	for _, tc := range cases {
		got, ok := InstalledFloor([]byte(tc.json))
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: InstalledFloor = %q,%v want %q,%v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

// TestEnginesSatisfies pins review M3-18: the whole range is evaluated, every
// || alternative and every comparator in it, with strict operators strict.
func TestEnginesSatisfies(t *testing.T) {
	const openclaw = ">=24.16.0<25||>=26.1.0"
	cases := []struct {
		rng, version string
		sat, ok      bool
	}{
		{openclaw, "v22.23.3", false, true},
		{openclaw, "v24.16.0", true, true},
		{openclaw, "v24.99.1", true, true},
		{openclaw, "v25.0.0", false, true}, // the hole the floor alone misses
		{openclaw, "v26.0.9", false, true},
		{openclaw, "v26.1.0", true, true},
		{">=24.16.0 <25 || >=26.1.0", "v25.2.0", false, true},
		{">20", "v20.9.9", false, true},
		{">20", "v21.0.0", true, true},
		{">20.1.2", "v20.1.2", false, true},
		{">20.1.2", "v20.1.3", true, true},
		{"<=20", "v20.9.0", true, true},
		{"<=20", "v21.0.0", false, true},
		{"^20.1.0", "v20.9.0", true, true},
		{"^20.1.0", "v21.0.0", false, true},
		{"^0.2.3", "v0.3.0", false, true},
		{"~18.2", "v18.2.5", true, true},
		{"~18.2", "v18.3.0", false, true},
		{"20.x", "v20.5.0", true, true},
		{"20.x", "v21.0.0", false, true},
		{"1.2.3 - 2.3", "v2.3.9", true, true},
		{"1.2.3 - 2.3", "v2.4.0", false, true},
		{"*", "v1.0.0", true, true},
		{"latest", "v20.0.0", false, false},
		{">=20", "not a version", false, false},
	}
	for _, tc := range cases {
		sat, ok := EnginesSatisfies(tc.rng, tc.version)
		if sat != tc.sat || ok != tc.ok {
			t.Errorf("EnginesSatisfies(%q, %q) = %v,%v want %v,%v", tc.rng, tc.version, sat, ok, tc.sat, tc.ok)
		}
	}
}

// TestInstalledEngines: the installed range rides along with its floor.
func TestInstalledEngines(t *testing.T) {
	floor, rng, ok := InstalledEngines([]byte(`{"engines":{"node":" >=24.16.0<25||>=26.1.0 "}}`))
	if !ok || floor != "24.16.0" || rng != ">=24.16.0<25||>=26.1.0" {
		t.Fatalf("InstalledEngines = %q,%q,%v", floor, rng, ok)
	}
	if _, _, ok := InstalledEngines([]byte(`{"engines":{"node":"*"}}`)); ok {
		t.Fatal("a range with no floor must report ok=false")
	}
}

// TestDirDependentInterpreter: version-manager shims, by documented location.
func TestDirDependentInterpreter(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/home/u/.asdf/shims/node", true},
		{"/home/u/.local/share/mise/shims/node", true},
		{"/home/u/.nodenv/shims/node", true},
		{"/home/u/.volta/bin/node", true},
		{`C:\Users\u\.volta\bin\node.exe`, true},
		{"/usr/bin/node", false},
		{"/home/u/.nvm/versions/node/v20.11.0/bin/node", false},
		{"/home/u/shims", false}, // the binary's own name is not a directory
		{"", false},
	}
	for _, tc := range cases {
		if got := DirDependentInterpreter(tc.path); got != tc.want {
			t.Errorf("DirDependentInterpreter(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestCheckRuntimeRangeAndDirectory: the two M3-18 rows of the status table.
func TestCheckRuntimeRangeAndDirectory(t *testing.T) {
	req := integration.RuntimeRequirement{Runtime: integration.RuntimeNode, MinVersion: "24.16.0", Range: ">=24.16.0 <25 || >=26.1.0", Product: "OpenClaw"}
	hole := CheckRuntime(req, RuntimeObservation{Path: "/usr/bin/node", Output: "v25.2.0"}, "linux", PackageManager{})
	if hole.Status != RuntimeOutsideRange || hole.Blocking() || hole.Range != req.Range {
		t.Fatalf("hole = %+v", hole)
	}
	if want := "OpenClaw declares Node.js >=24.16.0 <25 || >=26.1.0. This machine has 25.2.0"; !strings.HasPrefix(hole.Summary, want) {
		t.Fatalf("summary = %q", hole.Summary)
	}
	if ok := CheckRuntime(req, RuntimeObservation{Path: "/usr/bin/node", Output: "v26.1.0"}, "linux", PackageManager{}); ok.Status != RuntimeOK {
		t.Fatalf("in range = %s", ok.Status)
	}
	if old := CheckRuntime(req, RuntimeObservation{Path: "/usr/bin/node", Output: "v22.0.0"}, "linux", PackageManager{}); old.Status != RuntimeTooOld {
		t.Fatalf("below the floor = %s", old.Status)
	}
	shim := CheckRuntime(req, RuntimeObservation{Path: "/home/u/.asdf/shims/node", DirDependent: true}, "linux", PackageManager{})
	if shim.Status != RuntimeUnknown || !shim.DirDependent || shim.Blocking() || shim.Have != "" {
		t.Fatalf("dir-dependent = %+v", shim)
	}
}
