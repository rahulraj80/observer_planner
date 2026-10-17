package shellwrap

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

// TestUnavailableRules pins one case per reason-table row.
func TestUnavailableRules(t *testing.T) {
	gui, term := integration.LaunchKindGUI, integration.LaunchKindTerminal
	cases := []struct {
		name string
		in   unavailableInput
		want string
	}{
		{"ungrounded app", unavailableInput{kind: gui, grounded: false, names: []string{"x"}}, "not grounded on any OS"},
		{"no names for the OS", unavailableInput{kind: gui, grounded: true, probeOnly: true, osLabel: "Linux"}, "no grounded Linux command name"},
		{"probe-only app", unavailableInput{kind: gui, grounded: true, probeOnly: true, names: []string{"x"}}, "not as a command on PATH"},
		{"terminal row with no names", unavailableInput{kind: term, grounded: true, osLabel: "Windows"}, "no grounded Windows command name"},
		{"unshimmable name", unavailableInput{kind: term, grounded: true, names: []string{"a b"}}, "cannot be written as a shim"},
	}
	for _, c := range cases {
		if got := unavailableReason(c.in); !strings.Contains(got, c.want) {
			t.Errorf("%s: reason %q, want it to contain %q", c.name, got, c.want)
		}
	}
}

// TestEveryLaunchableRowIsCandidateOrUnavailable is the T-492 guarantee: on
// every target, each advertised launchable row is a wrap candidate or is
// listed as unavailable with a reason, never both and never neither.
func TestEveryLaunchableRowIsCandidateOrUnavailable(t *testing.T) {
	for _, h := range []Host{{GOOS: "linux"}, {GOOS: "darwin"}, {GOOS: "windows"}} {
		seen := map[string]string{}
		for _, c := range CandidatesOn(h) {
			seen[c.ID] = "candidate"
		}
		for _, u := range UnavailableOn(h) {
			if seen[u.ID] != "" {
				t.Errorf("%s: %s is both a candidate and unavailable", h.GOOS, u.ID)
			}
			if strings.TrimSpace(u.Reason) == "" {
				t.Errorf("%s: %s is unavailable with no reason", h.GOOS, u.ID)
			}
			seen[u.ID] = "unavailable"
		}
		for _, id := range integration.Tools() {
			c, _ := integration.For(id)
			if integration.TerminalLaunchable(c) && seen[id] == "" {
				t.Errorf("%s: terminal row %s is silently missing", h.GOOS, id)
			}
		}
		for _, g := range integration.GUILaunchables() {
			if g.Lifecycle.Advertised() && seen[g.Spec.ID] == "" {
				t.Errorf("%s: GUI row %s is silently missing", h.GOOS, g.Spec.ID)
			}
		}
	}
}

// TestDesktopAppsWithoutALinuxCommandAreListed names the R120 remainder:
// the desktop apps with no grounded Linux launch command each appear on the
// Linux target with a reason (T-492).
func TestDesktopAppsWithoutALinuxCommandAreListed(t *testing.T) {
	byID := map[string]Unavailable{}
	for _, u := range UnavailableOn(Host{GOOS: "linux"}) {
		byID[u.ID] = u
	}
	for _, id := range []string{
		"claude-desktop", "factory-desktop", "opencode-desktop", "zed-ide",
		"goose-desktop", "grokbot-desktop", "hermes-desktop",
	} {
		u, ok := byID[id]
		if !ok {
			t.Errorf("%s: not listed as unavailable on linux", id)
			continue
		}
		if u.Kind != integration.LaunchKindGUI || u.Label == "" || u.Reason == "" {
			t.Errorf("%s: incomplete entry %+v", id, u)
		}
		g, _ := integration.GUILaunchFor(id)
		if u.Hint != "" && len(g.Spec.Binary.Names.Unix) == 0 {
			t.Errorf("%s: hint %q offered with no Linux launch path", id, u.Hint)
		}
	}
}
