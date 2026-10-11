package toolinstall

import (
	"errors"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

var geminiNode = integration.RuntimeRequirement{Runtime: integration.RuntimeNode, MinVersion: "20", Product: "Gemini CLI", Source: "test"}

var aptPM = PackageManager{Name: "apt", Install: "sudo apt-get install -y", Family: "Debian/Ubuntu"}

// TestCheckRuntimeTable walks the ordered status table, one case per row plus
// the version-comparison edges.
func TestCheckRuntimeTable(t *testing.T) {
	pi := integration.RuntimeRequirement{Runtime: integration.RuntimeNode, MinVersion: "22.19.0", Product: "pi", Source: "test"}
	cases := []struct {
		name string
		req  integration.RuntimeRequirement
		obs  RuntimeObservation
		want RuntimeStatus
		have string
	}{
		{"no interpreter found is missing, never a guess", geminiNode, RuntimeObservation{}, RuntimeMissing, ""},
		{"probe error is unknown", geminiNode, RuntimeObservation{Path: "/usr/bin/node", ProbeErr: errors.New("timeout")}, RuntimeUnknown, ""},
		{"unparsable output is unknown", geminiNode, RuntimeObservation{Path: "/usr/bin/node", Output: "hello"}, RuntimeUnknown, ""},
		{"node 18 is too old for 20 (node 1, 2026-10-04)", geminiNode, RuntimeObservation{Path: "/usr/bin/node", Output: "v18.19.1\n"}, RuntimeTooOld, "18.19.1"},
		{"exactly the floor is ok", geminiNode, RuntimeObservation{Path: "/n", Output: "v20.0.0"}, RuntimeOK, "20.0.0"},
		{"newer major is ok", geminiNode, RuntimeObservation{Path: "/n", Output: "v24.21.0"}, RuntimeOK, "24.21.0"},
		{"minor below a full floor is too old", pi, RuntimeObservation{Path: "/n", Output: "v22.18.9"}, RuntimeTooOld, "22.18.9"},
		{"minor at a full floor is ok", pi, RuntimeObservation{Path: "/n", Output: "v22.19.0"}, RuntimeOK, "22.19.0"},
		{"unknown runtime in the requirement is unknown", integration.RuntimeRequirement{Runtime: "deno", MinVersion: "2", Product: "x"}, RuntimeObservation{Path: "/n", Output: "v1"}, RuntimeUnknown, ""},
		{"unparsable floor is unknown, never blocking", integration.RuntimeRequirement{Runtime: integration.RuntimeNode, MinVersion: "lts", Product: "x"}, RuntimeObservation{Path: "/n", Output: "v18.0.0"}, RuntimeUnknown, "18.0.0"},
	}
	for _, tc := range cases {
		got := CheckRuntime(tc.req, tc.obs, "linux", aptPM)
		if got.Status != tc.want {
			t.Errorf("%s: status %s, want %s", tc.name, got.Status, tc.want)
		}
		if got.Have != tc.have {
			t.Errorf("%s: have %q, want %q", tc.name, got.Have, tc.have)
		}
		if got.Blocking() != (tc.want == RuntimeTooOld) {
			t.Errorf("%s: Blocking() = %v; only too_old blocks", tc.name, got.Blocking())
		}
		if (got.Remedy != "") != (tc.want == RuntimeTooOld) {
			t.Errorf("%s: remedy %q; only too_old carries one", tc.name, got.Remedy)
		}
	}
}

// TestRuntimeRemedySentence pins the user sentence: product, floor, what this
// machine has, and a grounded way to get a newer runtime on THIS host family.
func TestRuntimeRemedySentence(t *testing.T) {
	obs := RuntimeObservation{Path: "/usr/bin/node", Output: "v18.19.1"}
	cases := []struct {
		name string
		goos string
		pm   PackageManager
		want []string
	}{
		{"debian/ubuntu: NodeSource LTS, not the distro package", "linux", aptPM, []string{
			"Gemini CLI needs Node.js 20 or newer. This machine has 18.19.1 (/usr/bin/node).\n",
			"\nInstall a newer Node.js first:\n",
			"https://deb.nodesource.com/setup_lts.x",
			"sudo apt-get install -y nodejs",
		}},
		{"fedora/rhel", "linux", PackageManager{Name: "dnf"}, []string{"https://rpm.nodesource.com/setup_lts.x", "sudo dnf install -y nodejs"}},
		{"macOS", "darwin", PackageManager{Name: "brew"}, []string{"brew install node"}},
		{"windows", "windows", PackageManager{}, []string{"winget install OpenJS.NodeJS.LTS"}},
		{"unknown linux family falls back to nodejs.org", "linux", PackageManager{}, []string{"https://nodejs.org/en/download"}},
	}
	for _, tc := range cases {
		got := CheckRuntime(geminiNode, obs, tc.goos, tc.pm).Remedy
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: remedy %q misses %q", tc.name, got, w)
			}
		}
		if strings.ContainsRune(got, '—') {
			t.Errorf("%s: remedy carries an em dash: %q", tc.name, got)
		}
	}
}

func TestRuntimeMinDisplay(t *testing.T) {
	cases := map[string]string{"20": "20", "20.0.0": "20", "22.0.0": "22", "22.19.0": "22.19", "24.16.0": "24.16", "22.19.1": "22.19.1"}
	for in, want := range cases {
		if got := DisplayRuntimeMin(in); got != want {
			t.Errorf("DisplayRuntimeMin(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseRuntimeVersion(t *testing.T) {
	cases := []struct {
		in   string
		want [3]int
		ok   bool
	}{
		{"v18.19.1\n", [3]int{18, 19, 1}, true},
		{"20", [3]int{20, 0, 0}, true},
		{"v22.19", [3]int{22, 19, 0}, true},
		{"v26.0.0-nightly2026", [3]int{26, 0, 0}, true},
		{"", [3]int{}, false},
		{"node", [3]int{}, false},
	}
	for _, tc := range cases {
		got, ok := ParseRuntimeVersion(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseRuntimeVersion(%q) = %v,%v want %v,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// TestChannelHostRuntime: only a channel whose installed program runs on the
// host's own interpreter is checked at install time. A vendor script or an OS
// package manager brings its own runtime (or none).
func TestChannelHostRuntime(t *testing.T) {
	for _, ch := range Channels() {
		want := ""
		if ch == "npm" {
			want = integration.RuntimeNode
		}
		if got := ChannelHostRuntime(ch); got != want {
			t.Errorf("ChannelHostRuntime(%q) = %q, want %q", ch, got, want)
		}
	}
	if got := ChannelHostRuntime("nope"); got != "" {
		t.Errorf("unknown channel = %q", got)
	}
}

func TestRuntimeProgram(t *testing.T) {
	if got := RuntimeProgram(integration.RuntimeNode, "linux"); got != "node" {
		t.Errorf("linux node = %q", got)
	}
	if got := RuntimeProgram(integration.RuntimeNode, "windows"); got != "node.exe" {
		t.Errorf("windows node = %q", got)
	}
	if got := RuntimeProgram("deno", "linux"); got != "" {
		t.Errorf("unknown runtime = %q", got)
	}
}

// TestRuntimeRemedyParts pins the structured split a UI renders (short
// sentence, then the command on its own, then one note) and the plain-text
// form with the command on a line of its own.
func TestRuntimeRemedyParts(t *testing.T) {
	obs := RuntimeObservation{Path: "/usr/bin/node", Output: "v18.19.1"}
	c := CheckRuntime(geminiNode, obs, "linux", aptPM)
	if c.Summary != "Gemini CLI needs Node.js 20 or newer. This machine has 18.19.1." {
		t.Errorf("Summary = %q", c.Summary)
	}
	if c.FixCommand != "curl -fsSL https://deb.nodesource.com/setup_lts.x | sudo -E bash - && sudo apt-get install -y nodejs" {
		t.Errorf("FixCommand = %q", c.FixCommand)
	}
	if !strings.Contains(c.FixNote, "distribution's own nodejs package is too old") {
		t.Errorf("FixNote = %q", c.FixNote)
	}
	want := c.Summary[:len(c.Summary)-1] + " (/usr/bin/node).\nInstall a newer Node.js first:\n  " + c.FixCommand + "\n" + c.FixNote
	if c.Remedy != want {
		t.Errorf("Remedy =\n%s\nwant\n%s", c.Remedy, want)
	}
	if c.FloorFrom != FloorFromRegistry {
		t.Errorf("FloorFrom = %q", c.FloorFrom)
	}
	// No grounded command for the host: the note is the whole advice.
	c = CheckRuntime(geminiNode, obs, "linux", PackageManager{})
	if c.FixCommand != "" || !strings.HasSuffix(c.Remedy, "Install a newer Node.js first. Download the LTS release from https://nodejs.org/en/download.") {
		t.Errorf("fallback: command %q remedy %q", c.FixCommand, c.Remedy)
	}
	// An ok check carries no fix parts.
	ok := CheckRuntime(geminiNode, RuntimeObservation{Path: "/n", Output: "v22.23.3"}, "linux", aptPM)
	if ok.Summary != "" || ok.FixCommand != "" || ok.FixNote != "" || ok.Remedy != "" {
		t.Errorf("ok check carries fix parts: %+v", ok)
	}
}
