package toolinstall

import (
	"sort"
	"testing"
	"time"
)

// TestLifecycleTransitions walks the lifecycle table one row per pair, plus
// the refusals that make the table load-bearing.
func TestLifecycleTransitions(t *testing.T) {
	cases := []struct {
		from Status
		ev   Event
		to   Status
		ok   bool
	}{
		// install exits
		{StatusInstalling, EventExitOK, StatusInstalled, true},
		{StatusInstalling, EventExitOKNoBinary, StatusInstalledNotFound, true},
		{StatusInstalling, EventExitFail, StatusInstallFailed, true},
		{StatusInstalling, EventOrphaned, StatusInstallInterrupted, true},
		// uninstall exits
		{StatusUninstalling, EventExitOK, StatusUninstalled, true},
		{StatusUninstalling, EventExitFail, StatusUninstallFailed, true},
		{StatusUninstalling, EventOrphaned, StatusUninstallInterrupted, true},
		// uninstall starts
		{StatusInstalled, EventUninstallStart, StatusUninstalling, true},
		{StatusInstalledNotFound, EventUninstallStart, StatusUninstalling, true},
		{StatusInstallInterrupted, EventUninstallStart, StatusUninstalling, true},
		{StatusUninstallFailed, EventUninstallStart, StatusUninstalling, true},
		{StatusUninstallInterrupted, EventUninstallStart, StatusUninstalling, true},
		// refusals
		{StatusInstallFailed, EventUninstallStart, "", false}, // nothing installed
		{StatusUninstalled, EventUninstallStart, "", false},   // already removed
		{StatusInstalling, EventUninstallStart, "", false},    // still running
		{StatusUninstalling, EventUninstallStart, "", false},  // double confirm
		{StatusInstalled, EventExitOK, "", false},             // no live PTY
		{StatusUninstalling, EventExitOKNoBinary, "", false},  // install-only event
		{StatusUninstalled, EventOrphaned, "", false},         // settled
		{Status("bogus"), EventUninstallStart, "", false},     // closed vocabulary
	}
	for _, tc := range cases {
		got, ok := Next(tc.from, tc.ev)
		if ok != tc.ok || got != tc.to {
			t.Errorf("Next(%s, %s) = (%q, %v), want (%q, %v)", tc.from, tc.ev, got, ok, tc.to, tc.ok)
		}
	}
}

func TestLifecyclePredicates(t *testing.T) {
	cases := []struct {
		s             Status
		inFlight      bool
		uninstallable bool
		forgettable   bool
	}{
		{StatusInstalling, true, false, false},
		{StatusInstalled, false, true, true},
		{StatusInstalledNotFound, false, true, true},
		{StatusInstallFailed, false, false, true},
		{StatusInstallInterrupted, false, true, true},
		{StatusUninstalling, true, false, false},
		{StatusUninstalled, false, false, true},
		{StatusUninstallFailed, false, true, true},
		{StatusUninstallInterrupted, false, true, true},
		{Status(""), false, false, false},
		{Status("bogus"), false, false, false},
	}
	for _, tc := range cases {
		if got := InFlight(tc.s); got != tc.inFlight {
			t.Errorf("InFlight(%q) = %v", tc.s, got)
		}
		if got := Uninstallable(tc.s); got != tc.uninstallable {
			t.Errorf("Uninstallable(%q) = %v", tc.s, got)
		}
		if got := Forgettable(tc.s); got != tc.forgettable {
			t.Errorf("Forgettable(%q) = %v", tc.s, got)
		}
	}
	// The CAS guard lists match the predicates exactly.
	from := UninstallableFrom()
	sort.Slice(from, func(i, j int) bool { return from[i] < from[j] })
	want := []Status{StatusInstallInterrupted, StatusInstalled, StatusInstalledNotFound, StatusUninstallFailed, StatusUninstallInterrupted}
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	if len(from) != len(want) {
		t.Fatalf("UninstallableFrom = %v, want %v", from, want)
	}
	for i := range from {
		if from[i] != want[i] {
			t.Fatalf("UninstallableFrom = %v, want %v", from, want)
		}
	}
	for _, s := range ForgettableFrom() {
		if InFlight(s) {
			t.Errorf("ForgettableFrom includes in-flight %q", s)
		}
	}
	if len(ForgettableFrom()) != 7 {
		t.Errorf("ForgettableFrom = %v, want the 7 settled statuses", ForgettableFrom())
	}
	if ExitEvent(0) != EventExitOK || ExitEvent(1) != EventExitFail || ExitEvent(-1) != EventExitFail {
		t.Error("ExitEvent mapping wrong")
	}
}

// TestInstallExitEvent: exit 0 is "installed" only when the binary resolves
// afterwards (T-034j); one row per outcome.
func TestInstallExitEvent(t *testing.T) {
	cases := []struct {
		code     int
		resolves bool
		want     Event
	}{
		{0, true, EventExitOK},
		{0, false, EventExitOKNoBinary},
		{1, true, EventExitFail},
		{1, false, EventExitFail},
		{-1, false, EventExitFail},
	}
	for _, tc := range cases {
		if got := InstallExitEvent(tc.code, tc.resolves); got != tc.want {
			t.Errorf("InstallExitEvent(%d, %v) = %s, want %s", tc.code, tc.resolves, got, tc.want)
		}
	}
}

// TestMaybeWaitingForInput: an in-flight run past InputWaitAfter is flagged;
// a settled one, a young one and one with no start time never are.
func TestMaybeWaitingForInput(t *testing.T) {
	start := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		s     Status
		start time.Time
		now   time.Time
		want  bool
	}{
		{"installing, young", StatusInstalling, start, start.Add(InputWaitAfter - time.Second), false},
		{"installing, past the wait", StatusInstalling, start, start.Add(InputWaitAfter), true},
		{"uninstalling, past the wait", StatusUninstalling, start, start.Add(10 * time.Minute), true},
		{"settled", StatusInstalled, start, start.Add(time.Hour), false},
		{"not found settled", StatusInstalledNotFound, start, start.Add(time.Hour), false},
		{"no start time", StatusInstalling, time.Time{}, start, false},
	}
	for _, tc := range cases {
		if got := MaybeWaitingForInput(tc.s, tc.start, tc.now); got != tc.want {
			t.Errorf("%s: MaybeWaitingForInput = %v, want %v", tc.name, got, tc.want)
		}
	}
}
