package planlimits

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestShouldPollLadder(t *testing.T) {
	oauth, _ := SourceByID(SourceClaudeOAuth)
	api, _ := SourceByID(SourceOpenRouter)
	cases := []struct {
		name    string
		in      LadderInput
		poll    bool
		reasonC string
	}{
		{"official always", LadderInput{Source: api, Now: t0}, true, ""},
		{"experimental needs opt-in", LadderInput{Source: oauth, Now: t0}, false, StatusNotOptedIn},
		{"fresh rung 1 covers it", LadderInput{Source: oauth, OptedIn: true, OfficialFreshAt: t0.Add(-10 * time.Minute), Now: t0}, false, StatusSkipped},
		{"stale rung 1 falls back", LadderInput{Source: oauth, OptedIn: true, OfficialFreshAt: t0.Add(-2 * time.Hour), Now: t0}, true, ""},
		{"no rung 1 falls back", LadderInput{Source: oauth, OptedIn: true, Now: t0}, true, ""},
	}
	for _, c := range cases {
		poll, reason := ShouldPoll(c.in)
		if poll != c.poll || reason != c.reasonC {
			t.Errorf("%s: %v %q", c.name, poll, reason)
		}
	}
}

func TestCadence(t *testing.T) {
	cases := []struct {
		in   CadenceInput
		want time.Duration
	}{
		{CadenceInput{PageOpen: true}, 2 * time.Minute},
		{CadenceInput{PageOpen: true, MinInterval: 10 * time.Minute}, 10 * time.Minute},
		{CadenceInput{NearLimit: true}, 5 * time.Minute},
		{CadenceInput{}, 30 * time.Minute},
	}
	for _, c := range cases {
		if got := Cadence(c.in); got != c.want {
			t.Errorf("%+v: %s want %s", c.in, got, c.want)
		}
	}
}

func TestNextRunBackoffTable(t *testing.T) {
	cad := 30 * time.Minute
	cases := []struct {
		name    string
		prev    RunState
		o       Outcome
		status  string
		stopped bool
		wait    time.Duration
	}{
		{"success", RunState{Failures: 3}, Outcome{}, StatusOK, false, cad},
		{"auth stops", RunState{}, Outcome{Err: fmt.Errorf("x: %w", ErrAuth)}, StatusAuthNeeded, true, 0},
		{"429 honours Retry-After", RunState{}, Outcome{Err: ErrRateLimited, RetryAfter: 90 * time.Second}, StatusThrottled, false, 90 * time.Second},
		{"429 caps Retry-After", RunState{}, Outcome{Err: ErrRateLimited, RetryAfter: 5 * time.Hour}, StatusThrottled, false, time.Hour},
		{"429 backs off", RunState{Failures: 1}, Outcome{Err: ErrRateLimited}, StatusThrottled, false, 4 * time.Minute},
		{"transient backs off", RunState{Failures: 2}, Outcome{Err: ErrTransient}, StatusError, false, 8 * time.Minute},
		{"unknown error is transient", RunState{Failures: 9}, Outcome{Err: errors.New("boom")}, StatusError, false, time.Hour},
		{"shape counts", RunState{ShapeFailures: 1}, Outcome{Err: ErrShape}, StatusEndpointGone, false, 2 * time.Minute},
		{"third shape stops", RunState{ShapeFailures: 2}, Outcome{Err: ErrGone}, StatusEndpointGone, true, 2 * time.Minute},
		{"not applicable", RunState{}, Outcome{Err: ErrNotApplicable}, "not_applicable", false, 6 * time.Hour},
		{"unavailable", RunState{}, Outcome{Err: ErrUnavailable}, "unavailable", false, time.Hour},
	}
	for _, c := range cases {
		got := NextRun(c.prev, c.o, t0, cad)
		if got.Status != c.status || got.Stopped != c.stopped || got.NextDue.Sub(t0) != c.wait {
			t.Errorf("%s: %+v (wait %s)", c.name, got, got.NextDue.Sub(t0))
		}
	}
	if OutcomeCode(ErrAuth) != StatusAuthNeeded || OutcomeCode(nil) != StatusOK {
		t.Error("outcome codes")
	}
}

func snapW(key string, frac float64, reset time.Time) Snapshot {
	s := newSnap(ProviderChatGPT, SourceChatGPTWham, KindBorrowedToken, ConfPercentOnly, t0)
	s.Windows = []Window{{Key: key, Unit: UnitPercent, UsedFrac: F(frac), ResetsAt: &reset}}
	return s
}

func TestSuspiciousResetHold(t *testing.T) {
	later := t0.Add(48 * time.Hour)
	prev := snapW("weekly", 0.8, later)
	if !Suspicious(prev, snapW("weekly", 0.1, later), t0) {
		t.Error("a sharp drop before the stated reset must be held")
	}
	if Suspicious(prev, snapW("weekly", 0.7, later), t0) {
		t.Error("a small drop is not suspicious")
	}
	due := snapW("weekly", 0.8, t0.Add(-time.Minute))
	if Suspicious(due, snapW("weekly", 0.0, later), t0) {
		t.Error("a drop after the stated reset is a real reset")
	}
	if !Agrees(snapW("weekly", 0.1, later), snapW("weekly", 0.12, later)) || Agrees(snapW("weekly", 0.1, later), snapW("weekly", 0.3, later)) {
		t.Error("agreement within five points")
	}
}

func TestMergeFreshestWinsManualOnlyFills(t *testing.T) {
	proxy, _ := FromLimitRow(LimitRow{Provider: "anthropic", ObservedAt: t0, Window5hUtil: fp(0.40), Window7dUtil: fp(0.20)})
	status := newSnap(ProviderClaude, SourceClaudeStatusLine, KindStatusLine, ConfPercentOnly, t0.Add(2*time.Minute))
	status.Windows = []Window{{Key: "5h", Unit: UnitPercent, UsedFrac: fp(0.45)}}
	manual := newSnap(ProviderClaude, SourceManual, KindManual, ConfEstimated, t0.Add(time.Hour))
	manual.PlanCode = "max_5x"
	manual.Windows = []Window{{Key: "5h", Unit: UnitPercent, UsedFrac: fp(0.99)}, {Key: "monthly", Unit: UnitPercent, UsedFrac: fp(0.10)}}
	m := Merge([]Snapshot{proxy, status, manual})
	w5, _ := FindWindow(m.Snapshot, "5h")
	if !approx(*w5.UsedFrac, 0.45) || m.WindowProv["5h"].Source != SourceClaudeStatusLine {
		t.Errorf("5h from the newer status line, got %v via %s", *w5.UsedFrac, m.WindowProv["5h"].Source)
	}
	if m.WindowProv["weekly"].Source != SourceClaudeProxy || m.WindowProv["monthly"].Source != SourceManual {
		t.Errorf("provenance %+v", m.WindowProv)
	}
	if m.Source != SourceClaudeStatusLine || m.PlanCode != "max_5x" {
		t.Errorf("header %+v", m.Snapshot)
	}
}

func TestPace(t *testing.T) {
	length := 7 * 24 * time.Hour
	reset := t0.Add(length / 2) // half way through
	w := Window{Key: "weekly", Unit: UnitPercent, UsedFrac: F(0.6), ResetsAt: &reset, Length: &length}
	p := PaceOf(w, t0)
	if p == nil || !approx(p.ProjectedFrac, 1.2) || p.RunsOutAt == nil {
		t.Fatalf("pace %+v", p)
	}
	if got := p.RunsOutAt.Sub(t0); got < 56*time.Hour-time.Minute || got > 56*time.Hour+time.Minute {
		t.Errorf("runs out in %s, want about 56h", got)
	}
	w.UsedFrac = F(0.2)
	if p := PaceOf(w, t0); p == nil || p.RunsOutAt != nil {
		t.Errorf("on track %+v", p)
	}
	young := t0.Add(length - time.Minute)
	w.ResetsAt = &young
	if PaceOf(w, t0) != nil {
		t.Error("a window this young is not projected")
	}
	if !Expired(Window{ResetsAt: &t0}, t0) || Expired(Window{}, t0) {
		t.Error("expiry")
	}
}
