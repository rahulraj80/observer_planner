package updatespec

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Channel is a release channel. Channel is a property of the MANIFEST,
// not of the binary: the same version can be published on more than one
// channel at different times (§1, open decision O2 owns the names).
type Channel string

const (
	// ChannelStable is the default channel every node gets unless the
	// org assigns another.
	ChannelStable Channel = "stable"
	// ChannelLTS is the long-term-support channel. Pinning a fleet here
	// implies a support-duration promise the operator doc must state.
	ChannelLTS Channel = "lts"
	// ChannelEdge is the fast channel, for a canary fleet.
	ChannelEdge Channel = "edge"
)

// KnownChannel reports whether c is one of the three published
// channels. An unknown channel is a validation failure, never a silent
// pass: a typo'd channel would publish a manifest no node ever asks for.
func KnownChannel(c Channel) bool {
	switch c {
	case ChannelStable, ChannelLTS, ChannelEdge:
		return true
	default:
		return false
	}
}

// Window is a local maintenance window, e.g. "02:00-05:00" in the
// machine's own timezone. The zero value is "any time", which is what
// an empty [update].window means.
//
// A window that wraps midnight ("22:00-02:00") is normal on a fleet
// spread across timezones and is handled explicitly rather than by
// accident.
type Window struct {
	// startMin and endMin are minutes since local midnight.
	startMin, endMin int
	// set distinguishes a configured window from the zero value.
	set bool
	// raw is the text the window was parsed from, for display.
	raw string
}

// ParseWindow parses "HH:MM-HH:MM". An empty string yields the zero
// window (always permitted), which is the documented meaning of an
// empty [update].window.
func ParseWindow(s string) (Window, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Window{}, nil
	}
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return Window{}, fmt.Errorf("update.ParseWindow: %q is not HH:MM-HH:MM", s)
	}
	start, err := parseClock(parts[0])
	if err != nil {
		return Window{}, fmt.Errorf("update.ParseWindow: %q: %w", s, err)
	}
	end, err := parseClock(parts[1])
	if err != nil {
		return Window{}, fmt.Errorf("update.ParseWindow: %q: %w", s, err)
	}
	if start == end {
		return Window{}, fmt.Errorf("update.ParseWindow: %q is a zero-length window", s)
	}
	return Window{startMin: start, endMin: end, set: true, raw: s}, nil
}

// parseClock parses "HH:MM" into minutes since midnight.
func parseClock(s string) (int, error) {
	s = strings.TrimSpace(s)
	hm := strings.Split(s, ":")
	if len(hm) != 2 {
		return 0, fmt.Errorf("%q is not HH:MM", s)
	}
	h, err := strconv.Atoi(hm[0])
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("%q has an out-of-range hour", s)
	}
	m, err := strconv.Atoi(hm[1])
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("%q has an out-of-range minute", s)
	}
	return h*60 + m, nil
}

// IsSet reports whether a window was configured.
func (w Window) IsSet() bool { return w.set }

// String renders the window as configured, or "any time".
func (w Window) String() string {
	if !w.set {
		return "any time"
	}
	return w.raw
}

// Permits reports whether t (in its own location) falls inside the
// window. The zero window permits everything.
func (w Window) Permits(t time.Time) bool {
	if !w.set {
		return true
	}
	if t.IsZero() {
		return false
	}
	mins := t.Hour()*60 + t.Minute()
	if w.startMin < w.endMin {
		return mins >= w.startMin && mins < w.endMin
	}
	// Wraps midnight.
	return mins >= w.startMin || mins < w.endMin
}

// NextOpen returns the next instant the window is open at or after t.
// When the window is open at t, t is returned unchanged, so a caller
// can print "next open" without special-casing "now".
func (w Window) NextOpen(t time.Time) time.Time {
	if !w.set || w.Permits(t) {
		return t
	}
	start := time.Date(t.Year(), t.Month(), t.Day(), w.startMin/60, w.startMin%60, 0, 0, t.Location())
	if !start.After(t) {
		start = start.AddDate(0, 0, 1)
	}
	return start
}
