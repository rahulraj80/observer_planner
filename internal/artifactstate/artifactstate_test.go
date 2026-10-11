package artifactstate

import (
	"strings"
	"testing"
)

const (
	now    = int64(10_000_000_000)
	window = int64(14 * 24 * 3600 * 1000)
)

func seen(version string, protocol int, ageMs int64) *Seen {
	return &Seen{Version: version, Protocol: protocol, LastSeenMs: now - ageMs}
}

// TestBrowserRules has one case per row of browserRules, in table order,
// plus the leftover overlay.
func TestBrowserRules(t *testing.T) {
	base := BrowserInput{ComponentActive: true, HostPresent: true, NowMs: now, ConnectedWindowMs: window, ProtocolMin: 1, ProtocolMax: 2, BinaryVersion: "v1.36.0"}
	with := func(f func(*BrowserInput)) BrowserInput { in := base; f(&in); return in }
	cases := []struct {
		name     string
		in       BrowserInput
		rule     string
		state    State
		leftover bool
		detail   string
	}{
		{"newer protocol refused", with(func(in *BrowserInput) { in.Seen = seen("1.40.0", 3, 1000) }), "newer-protocol", StateIncompatible, false, "refuses"},
		{"host removed, extension recent, component active", with(func(in *BrowserInput) { in.HostPresent = false; in.Seen = seen("1.36.0", 2, 60_000) }), "host-removed", StateHostRemoved, false, "probably still installed"},
		{"host removed, component inactive is a leftover", with(func(in *BrowserInput) {
			in.HostPresent = false
			in.ComponentActive = false
			in.Seen = seen("1.36.0", 2, 60_000)
		}), "host-removed", StateHostRemoved, true, "probably still installed"},
		{"no host, never seen", with(func(in *BrowserInput) { in.HostPresent = false }), "no-host", StateNotDetected, false, "no native-messaging host"},
		{"no host, seen long ago", with(func(in *BrowserInput) { in.HostPresent = false; in.Seen = seen("1.30.0", 2, 2*window) }), "no-host", StateNotDetected, false, "no native-messaging host"},
		{"host, never connected", base, "never-connected", StateNotDetected, false, "never connects"},
		{"host, seen long ago", with(func(in *BrowserInput) { in.Seen = seen("1.30.0", 2, 2*window) }), "not-recent", StateNotDetected, false, "28 days ago (version 1.30.0)"},
		{"old protocol", with(func(in *BrowserInput) { in.ProtocolMin = 2; in.Seen = seen("", 1, 1000) }), "old-protocol", StateStale, false, "protocol 1"},
		{"connected", with(func(in *BrowserInput) { in.Seen = seen("1.36.0", 2, 1000) }), "connected", StateConnected, false, "version 1.36.0, protocol 2"},
		{"connected legacy extension", with(func(in *BrowserInput) { in.Seen = seen("", 1, 1000) }), "connected", StateConnected, false, "does not report its version"},
		{"connected with release skew", with(func(in *BrowserInput) { in.Seen = seen("1.34.0", 2, 1000) }), "connected", StateConnected, false, "differs from this Observer"},
		{"connected while inactive is a leftover", with(func(in *BrowserInput) { in.ComponentActive = false; in.Seen = seen("1.36.0", 2, 1000) }), "connected", StateConnected, true, "version 1.36.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Browser("artifact.browser-extension", tc.in)
			if got.Rule != tc.rule || got.State != tc.state || got.Leftover != tc.leftover {
				t.Fatalf("got rule %s state %s leftover %v, want %s %s %v", got.Rule, got.State, got.Leftover, tc.rule, tc.state, tc.leftover)
			}
			if !strings.Contains(got.Detail, tc.detail) {
				t.Errorf("detail %q does not contain %q", got.Detail, tc.detail)
			}
		})
	}
	rules := map[string]bool{}
	for _, tc := range cases {
		rules[tc.rule] = true
	}
	for _, r := range browserRules {
		if !rules[r.name] {
			t.Errorf("browser rule %s has no test case", r.name)
		}
	}
}

func TestBrowserLeftoverRemedyNamesTeardown(t *testing.T) {
	got := Browser("x", BrowserInput{HostPresent: true, NowMs: now, ConnectedWindowMs: window, ProtocolMin: 1, ProtocolMax: 2})
	if !got.Leftover || !strings.Contains(got.Remedy, "observer components teardown capture.browser") {
		t.Fatalf("an inactive component with the host on disk must be a leftover naming the teardown: %+v", got)
	}
}

func TestEditorRules(t *testing.T) {
	cases := []struct {
		name  string
		in    EditorInput
		rule  string
		state State
	}{
		{"none", EditorInput{BinaryVersion: "v1.36.0"}, "none-found", StateNotDetected},
		{"older", EditorInput{BinaryVersion: "v1.36.0", Installs: []EditorInstall{{Editor: "VS Code", Version: "1.36.0"}, {Editor: "Cursor", Version: "1.34.0"}}}, "older-than-binary", StateStale},
		{"same", EditorInput{BinaryVersion: "v1.36.0-rc.2", Installs: []EditorInstall{{Editor: "VS Code", Version: "1.36.0"}}}, "installed", StateInstalled},
		{"newer", EditorInput{BinaryVersion: "v1.36.0", Installs: []EditorInstall{{Editor: "VS Code", Version: "1.37.0"}}}, "installed", StateInstalled},
		{"dev binary claims nothing", EditorInput{BinaryVersion: "dev", Installs: []EditorInstall{{Editor: "VS Code", Version: "1.1.0"}}}, "installed", StateInstalled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Editor("artifact.vscode-extension", tc.in)
			if got.Rule != tc.rule || got.State != tc.state {
				t.Fatalf("got %s %s, want %s %s", got.Rule, got.State, tc.rule, tc.state)
			}
			if len(got.Items) != len(tc.in.Installs) {
				t.Fatalf("items %d, installs %d", len(got.Items), len(tc.in.Installs))
			}
		})
	}
}

func TestEditorExtensionVersion(t *testing.T) {
	const id = "superbased.superbased-observer"
	cases := []struct {
		dir, want string
		ok        bool
	}{
		{"superbased.superbased-observer-1.36.0-linux-x64", "1.36.0", true},
		{"superbased.superbased-observer-1.36.0-rc.11-win32-x64", "1.36.0-rc.11", true},
		{"SuperBased.superbased-observer-1.36.0", "1.36.0", true},
		{"superbased.superbased-observer-1.36.0-darwin-arm64", "1.36.0", true},
		{"superbased.superbased-observer-extra-1.0.0", "", false},
		{"ms-python.python-2026.1.0", "", false},
		{"superbased.superbased-observer-1.36", "", false},
	}
	for _, tc := range cases {
		got, ok := EditorExtensionVersion(tc.dir, id)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: got %q %v, want %q %v", tc.dir, got, ok, tc.want, tc.ok)
		}
	}
}

func TestShimRules(t *testing.T) {
	act := ShimHost{Tool: "opencode", Path: "/p", Present: true, Ours: true, AdapterActive: true, HooksActive: true, Component: "adapter.opencode"}
	off := act
	off.Tool, off.AdapterActive, off.Component = "pi", false, "adapter.pi"
	foreign := ShimHost{Tool: "openclaw", Present: true}
	cases := []struct {
		name     string
		hosts    []ShimHost
		rule     string
		state    State
		leftover bool
	}{
		{"none", []ShimHost{{Tool: "opencode"}, foreign}, "none", StateAbsent, false},
		{"leftover", []ShimHost{act, off}, "leftover", StateInstalled, true},
		{"installed", []ShimHost{act, foreign}, "installed", StateInstalled, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Shims("artifact.plugin-shims", tc.hosts)
			if got.Rule != tc.rule || got.State != tc.state || got.Leftover != tc.leftover {
				t.Fatalf("got %+v", got)
			}
			if tc.leftover && !strings.Contains(got.Remedy, "teardown adapter.pi") {
				t.Errorf("remedy %q does not name the teardown", got.Remedy)
			}
		})
	}
}

func TestHelperRules(t *testing.T) {
	cases := []struct {
		name     string
		in       HelperInput
		rule     string
		state    State
		leftover bool
	}{
		{"not applicable", HelperInput{Present: true}, "not-applicable", StateNotApplicable, false},
		{"absent", HelperInput{Applicable: true, ComponentActive: true, Component: "processobs"}, "absent", StateAbsent, false},
		{"unused", HelperInput{Applicable: true, Present: true, Component: "processobs"}, "unused", StateInstalled, true},
		{"installed", HelperInput{Applicable: true, Present: true, ComponentActive: true}, "installed", StateInstalled, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Helper("artifact.node-inspection", tc.in)
			if got.Rule != tc.rule || got.State != tc.state || got.Leftover != tc.leftover {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestCompareCore(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"v1.36.0", "1.36.0-rc.1", 0, true},
		{"1.35.9", "1.36.0", -1, true},
		{"1.36.10", "1.36.9", 1, true},
		{"dev", "1.0.0", 0, false},
		{"", "1.0.0", 0, false},
	}
	for _, tc := range cases {
		got, ok := CompareCore(tc.a, tc.b)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s vs %s: got %d %v", tc.a, tc.b, got, ok)
		}
	}
}

// TestNoDashesInCopy keeps the copy free of em and en dashes (operator
// rule): every detail and remedy line a table can emit is rendered here.
func TestNoDashesInCopy(t *testing.T) {
	in := BrowserInput{NowMs: now, ConnectedWindowMs: window, ProtocolMin: 1, ProtocolMax: 2, BinaryVersion: "1.0.0"}
	var all []Status
	for _, hp := range []bool{true, false} {
		for _, act := range []bool{true, false} {
			for _, s := range []*Seen{nil, seen("2.0.0", 3, 1), seen("1.0.0", 2, 1), seen("", 0, 1), seen("1.0.0", 2, 3*window)} {
				x := in
				x.HostPresent, x.ComponentActive, x.Seen = hp, act, s
				all = append(all, Browser("b", x))
			}
		}
	}
	all = append(all, Editor("e", EditorInput{}), Editor("e", EditorInput{BinaryVersion: "2.0.0", Installs: []EditorInstall{{Version: "1.0.0"}}}))
	all = append(all, Shims("s", nil), Helper("h", HelperInput{}), Helper("h", HelperInput{Applicable: true, ComponentActive: true}), Helper("h", HelperInput{Applicable: true, Present: true}))
	for _, s := range all {
		for _, txt := range []string{s.Detail, s.Remedy} {
			if strings.ContainsAny(txt, "\u2014\u2013") {
				t.Errorf("%s: dash in %q", s.Rule, txt)
			}
		}
	}
}
