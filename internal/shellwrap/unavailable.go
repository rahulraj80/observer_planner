package shellwrap

import (
	"sort"

	"github.com/marmutapp/superbased-observer/internal/integration"
)

// Unavailable is one launchable row that cannot be command-wrapped on a
// target, with the reason (T-492). Every advertised row is either a Candidate
// or an Unavailable on every target, so a status listing never drops a tool
// silently (pinned by TestEveryLaunchableRowIsCandidateOrUnavailable).
type Unavailable struct {
	// ID is the registry tool key or GUI launch id.
	ID string `json:"id"`
	// Label is a display name (the GUI row's label; the id for a CLI row).
	Label string `json:"label"`
	// Kind is "terminal" or "gui".
	Kind integration.LaunchKind `json:"kind"`
	// Reason says why there is no command to wrap on this target.
	Reason string `json:"reason"`
	// Hint is the wrapped launch that still works, "" when none does.
	Hint string `json:"hint,omitempty"`
}

// unavailableInput is what the reason table sees for one row.
type unavailableInput struct {
	kind      integration.LaunchKind
	grounded  bool
	probeOnly bool
	names     []string // the row's spellings for the target OS
	osLabel   string
	// launchHere: the row has a launch path on this OS (a command name, a
	// macOS bundle, or a Windows packaged-app id), so `observer ide` works.
	launchHere bool
}

// unavailableRule is one row of the ordered reason table: first match wins,
// one test case per row (CLAUDE.md #5). No product name appears here; the
// facts come from the row's own registry data.
type unavailableRule struct {
	reason func(in unavailableInput) string
	match  func(in unavailableInput) bool
}

var unavailableRules = []unavailableRule{
	{
		func(unavailableInput) string {
			return "its desktop app is not grounded on any OS yet, so there is no launch command to wrap"
		},
		func(in unavailableInput) bool { return in.kind == integration.LaunchKindGUI && !in.grounded },
	},
	{
		func(in unavailableInput) string {
			return "no grounded " + in.osLabel + " command name to replace (no " + in.osLabel +
				" build, or its " + in.osLabel + " command is not verified)"
		},
		func(in unavailableInput) bool { return len(in.names) == 0 },
	},
	{
		func(unavailableInput) string {
			return "the app is found only in its install folder, not as a command on PATH (its name collides " +
				"with another program, or the app has no PATH alias), so there is no command to replace"
		},
		func(in unavailableInput) bool { return in.kind == integration.LaunchKindGUI && in.probeOnly },
	},
	{
		func(unavailableInput) string {
			return "its command name cannot be written as a shim (it contains a space or a quote)"
		},
		func(unavailableInput) bool { return true },
	},
}

// osLabels names a target OS for a reason line.
var osLabels = map[string]string{"linux": "Linux", "darwin": "macOS", "windows": "Windows"}

// UnavailableOn lists every advertised launchable row that CandidateOn does
// not return on the target h describes, sorted by ID. Rows whose product is
// not advertised (dead or retired) are left out, as everywhere else.
func UnavailableOn(h Host) []Unavailable {
	ids := append([]string{}, integration.Tools()...)
	for _, g := range integration.GUILaunchables() {
		ids = append(ids, g.Spec.ID)
	}
	out := []Unavailable{}
	for _, id := range dedupe(ids) {
		if _, ok := CandidateOn(id, h); ok {
			continue
		}
		if u, ok := unavailableFor(id, h); ok {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// unavailableFor builds one row's entry. ok=false for a row that is not
// advertised at all (not part of the wrap question).
func unavailableFor(id string, h Host) (Unavailable, bool) {
	label := osLabels[h.GOOS]
	if label == "" {
		label = h.GOOS
	}
	pick := func(n integration.BinaryNames) []string {
		if h.GOOS == "windows" {
			return n.Windows
		}
		return n.Unix
	}
	if c, ok := integration.For(id); ok {
		if !integration.TerminalLaunchable(c) {
			return Unavailable{}, false
		}
		in := unavailableInput{kind: integration.LaunchKindTerminal, grounded: true, osLabel: label}
		if c.Binary != nil {
			in.names = pick(c.Binary.Names)
		}
		return Unavailable{
			ID: id, Label: id, Kind: integration.LaunchKindTerminal, Reason: unavailableReason(in),
		}, true
	}
	g, ok := integration.GUILaunchFor(id)
	if !ok || !g.Lifecycle.Advertised() {
		return Unavailable{}, false
	}
	in := unavailableInput{
		kind: integration.LaunchKindGUI, grounded: g.Spec.Grounded, probeOnly: g.Spec.ProbeOnly,
		names: pick(g.Spec.Binary.Names), osLabel: label,
	}
	in.launchHere = len(in.names) > 0 || (h.GOOS == "darwin" && g.Spec.DarwinApp != "") ||
		(h.GOOS == "windows" && g.Spec.AppsFolderAUMID != "")
	u := Unavailable{ID: id, Label: g.Spec.Label, Kind: integration.LaunchKindGUI, Reason: unavailableReason(in)}
	if g.Advertised() && in.launchHere {
		u.Hint = "launch it wrapped with `observer " + integration.GUILaunchVerb + " " + id + "` or from the dashboard"
	}
	return u, true
}

// unavailableReason walks unavailableRules.
func unavailableReason(in unavailableInput) string {
	for _, r := range unavailableRules {
		if r.match(in) {
			return r.reason(in)
		}
	}
	return ""
}
