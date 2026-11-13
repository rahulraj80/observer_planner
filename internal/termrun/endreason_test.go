package termrun

import (
	"strings"
	"testing"
)

// TestEndReasonForExit pins the code-only classification (Windows 11 Pro
// run 4, N3), one row per shape.
func TestEndReasonForExit(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		code int
		want string
	}{
		{0, EndChildExit},
		{1, EndChildExit},
		{7, EndChildExit},
		{3221225786, EndConsoleClosed},  // 0xC000013A as GetExitCodeProcess reports it
		{-1073741510, EndConsoleClosed}, // the same NTSTATUS as a signed 32-bit code
		{4294967295, EndStoppedOutside}, // 0xFFFFFFFF
		{-1, EndStoppedOutside},
	} {
		if got := EndReasonForExit(c.code); got != c.want {
			t.Errorf("EndReasonForExit(%d) = %q, want %q", c.code, got, c.want)
		}
	}
}

// TestEndLineNamesEveryReason: every reason has its own named line, and the
// line carries the exit code when one was seen.
func TestEndLineNamesEveryReason(t *testing.T) {
	t.Parallel()
	one := 1
	want := map[string]string{
		EndChildExit:            "the run ended: the tool exited (exit code 1)",
		EndConsoleClosed:        "the run ended: the console window was closed (exit code 1)",
		EndDashboardStop:        "the run ended: stopped from the dashboard (Stop & close) (exit code 1)",
		EndCLIStop:              "the run ended: stopped with observer terminals stop (exit code 1)",
		EndLaunchRefused:        "the run ended: the launch was refused before the tool started (exit code 1)",
		EndStoppedOutside:       "the run ended: stopped from outside (for example Stop-Process, taskkill /F or a kill signal) (exit code 1)",
		EndProcessGone:          "the run ended: the run's process is gone (ended outside Observer); its exit code is not known (exit code 1)",
		EndDaemonShutdown:       "the run ended: the Observer daemon stopped (exit code 1)",
		EndResumed:              "the run ended: resumed in a newer run (exit code 1)",
		EndOrgFloorHooksDrifted: "the run ended: ended by the organization's full-capture floor (the run's hooks drifted) (exit code 1)",
	}
	seen := map[string]bool{}
	for r, w := range want {
		if !KnownEndReason(r) {
			t.Errorf("%q is not known", r)
		}
		got := EndLine(r, &one)
		if got != w {
			t.Errorf("EndLine(%q) = %q, want %q", r, got, w)
		}
		if seen[got] {
			t.Errorf("two reasons share the line %q", got)
		}
		seen[got] = true
	}
	if len(endLines) != len(want) {
		t.Errorf("%d lines, %d pinned", len(endLines), len(want))
	}
	if got := EndLine(EndProcessGone, nil); got != "the run ended: the run's process is gone (ended outside Observer); its exit code is not known" {
		t.Errorf("no code: %q", got)
	}
	if got := EndLine("something-new", nil); got != "the run ended" || KnownEndReason("something-new") {
		t.Errorf("unknown reason: %q", got)
	}
}

// TestEndReasonCarriesADetail (live run 2026-11-11): a reason can carry a
// short detail, one line and bounded; the code splits back off it, the
// named line shows the detail, and a reason outside the vocabulary or an
// empty detail is left as it is.
func TestEndReasonCarriesADetail(t *testing.T) {
	t.Parallel()
	r := WithEndDetail(EndLaunchRefused, "claude-code is not installed in the box,\r\nand the box has no npm\x00")
	if r != "launch_refused: claude-code is not installed in the box, and the box has no npm" {
		t.Fatalf("WithEndDetail = %q", r)
	}
	if code, detail := SplitEndReason(r); code != EndLaunchRefused || detail != "claude-code is not installed in the box, and the box has no npm" {
		t.Errorf("split %q %q", code, detail)
	}
	if !KnownEndReason(r) {
		t.Error("a detailed reason is not known by its code")
	}
	code := 125
	if got := EndLine(r, &code); got != "the run ended: the launch was refused before the tool started: claude-code is not installed in the box, and the box has no npm (exit code 125)" {
		t.Errorf("EndLine = %q", got)
	}
	if got := WithEndDetail(EndLaunchRefused, "  "); got != EndLaunchRefused {
		t.Errorf("empty detail: %q", got)
	}
	if got := WithEndDetail("not-a-reason", "x"); got != "not-a-reason" {
		t.Errorf("unknown reason: %q", got)
	}
	if code, detail := SplitEndReason("http: refused"); code != "http: refused" || detail != "" {
		t.Errorf("a head that is no code split: %q %q", code, detail)
	}
	// A node's refusal with its fix is kept whole (live run 2026-11-13).
	refusal := "create the box: the node refused the sandbox: your SandHold account needs attention: a card is needed to start more sandboxes: " +
		"you have started 5, and 5 may start without a card. Your first month stays free with a card on file. Running sandboxes, their files and exports are not affected " +
		"(to fix: add a card at https://sandhold.example:48000/app/#/billing (it is checked with a $1.00 charge that is refunded at once), then try again)"
	if _, d := SplitEndReason(WithEndDetail(EndLaunchRefused, refusal)); d != refusal {
		t.Errorf("a node's refusal was cut: %q", d)
	}
	long := WithEndDetail(EndLaunchRefused, strings.Repeat("x", 2*EndDetailMax))
	if _, d := SplitEndReason(long); len(d) != EndDetailMax+3 || !strings.HasSuffix(d, "...") {
		t.Errorf("long detail kept %d bytes", len(d))
	}
}
