package policyfresh

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/testsupport/importpin"
)

// TestDecideTable walks every row of the freshness table with the operator's
// defaults (30 s min interval, 0-5 s jitter, immediate after 5 min idle).
func TestDecideTable(t *testing.T) {
	t0 := time.Date(2026, 10, 2, 5, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		st       State
		trig     Trigger
		now      time.Time
		jitter   float64
		want     Action
		wantAt   time.Time
		wantHold bool
		wantRule string
	}{
		{name: "404 silences", st: State{UnsupportedUntil: t0.Add(time.Hour)}, trig: TriggerActivity, now: t0, want: ActionNone, wantRule: "unsupported"},
		{name: "404 also silences the backstop", st: State{UnsupportedUntil: t0.Add(time.Hour)}, trig: TriggerBackstop, now: t0, want: ActionNone, wantRule: "unsupported"},
		{name: "404 also stops a held re-arm", st: State{UnsupportedUntil: t0.Add(time.Hour), HoldSupported: true, LastActivity: t0}, trig: TriggerProbeDone, now: t0, want: ActionNone, wantRule: "unsupported"},
		{name: "after the 404 window it checks again", st: State{UnsupportedUntil: t0.Add(-time.Second)}, trig: TriggerBackstop, now: t0, want: ActionCheckNow, wantRule: "backstop_or_scheduled"},
		{name: "one in flight coalesces", st: State{InFlight: true}, trig: TriggerActivity, now: t0, want: ActionNone, wantRule: "in_flight"},
		{name: "held probe done, active, past the gap: re-arm now", st: State{LastCheck: t0.Add(-20 * time.Second), LastActivity: t0.Add(-3 * time.Second), HoldSupported: true}, trig: TriggerProbeDone, now: t0, want: ActionCheckNow, wantHold: true, wantRule: "probe_done"},
		{name: "held probe answered early: re-arm at the gap", st: State{LastCheck: t0.Add(-2 * time.Second), LastActivity: t0.Add(-1 * time.Second), HoldSupported: true}, trig: TriggerProbeDone, now: t0, want: ActionSchedule, wantAt: t0.Add(1 * time.Second), wantHold: true, wantRule: "probe_done"},
		{name: "failed probe is not re-armed", st: State{LastCheck: t0.Add(-20 * time.Second), LastActivity: t0, HoldSupported: true, LastProbeFailed: true}, trig: TriggerProbeDone, now: t0, want: ActionNone, wantRule: "probe_done"},
		{name: "server that does not hold: no re-arm (pre-hold cadence)", st: State{LastCheck: t0.Add(-20 * time.Second), LastActivity: t0}, trig: TriggerProbeDone, now: t0, want: ActionNone, wantRule: "probe_done"},
		{name: "idle node: no re-arm (idle cost unchanged)", st: State{LastCheck: t0.Add(-20 * time.Second), LastActivity: t0.Add(-6 * time.Minute), HoldSupported: true}, trig: TriggerProbeDone, now: t0, want: ActionNone, wantRule: "probe_done"},
		{name: "backstop checks without a hold", st: State{LastCheck: t0.Add(-10 * time.Second)}, trig: TriggerBackstop, now: t0, want: ActionCheckNow, wantRule: "backstop_or_scheduled"},
		{name: "scheduled checks with a hold", st: State{LastCheck: t0.Add(-31 * time.Second)}, trig: TriggerScheduled, now: t0, want: ActionCheckNow, wantHold: true, wantRule: "backstop_or_scheduled"},
		{name: "very first activity checks now", trig: TriggerActivity, now: t0, want: ActionCheckNow, wantHold: true, wantRule: "first_after_idle"},
		{name: "first activity after 5 min idle checks now", st: State{LastActivity: t0.Add(-6 * time.Minute), LastCheck: t0.Add(-6 * time.Minute)}, trig: TriggerActivity, now: t0, want: ActionCheckNow, wantHold: true, wantRule: "first_after_idle"},
		{name: "idle but the backstop just ran: schedule instead", st: State{LastActivity: t0.Add(-6 * time.Minute), LastCheck: t0.Add(-5 * time.Second)}, trig: TriggerActivity, now: t0, jitter: 0, want: ActionSchedule, wantAt: t0.Add(25 * time.Second), wantHold: true, wantRule: "min_interval"},
		{name: "busy within 30 s schedules at min interval plus jitter", st: State{LastActivity: t0.Add(-2 * time.Second), LastCheck: t0.Add(-10 * time.Second)}, trig: TriggerActivity, now: t0, jitter: 0.5, want: ActionSchedule, wantAt: t0.Add(20*time.Second + 2500*time.Millisecond), wantHold: true, wantRule: "min_interval"},
		{name: "already scheduled: activity joins it", st: State{LastActivity: t0.Add(-2 * time.Second), LastCheck: t0.Add(-10 * time.Second), Scheduled: t0.Add(20 * time.Second)}, trig: TriggerActivity, now: t0, jitter: 0.9, want: ActionNone, wantRule: "already_scheduled"},
		{name: "busy past 30 s checks now", st: State{LastActivity: t0.Add(-2 * time.Second), LastCheck: t0.Add(-31 * time.Second)}, trig: TriggerActivity, now: t0, want: ActionCheckNow, wantHold: true, wantRule: "default"},
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Decide(Config{}, tc.st, tc.trig, tc.now, tc.jitter)
			if d.Action != tc.want || d.Rule != tc.wantRule {
				t.Fatalf("decision = %+v, want action %v rule %s", d, tc.want, tc.wantRule)
			}
			if d.Hold != tc.wantHold {
				t.Fatalf("hold = %v, want %v (%+v)", d.Hold, tc.wantHold, d)
			}
			if !tc.wantAt.IsZero() && !d.At.Equal(tc.wantAt) {
				t.Fatalf("at = %v, want %v", d.At, tc.wantAt)
			}
		})
		seen[tc.wantRule] = true
	}
	for _, r := range rules {
		if !seen[r.name] {
			t.Errorf("rule %q has no case", r.name)
		}
	}
}

// TestRateLimitBound pins the volume claim: however dense the activity, an
// active node probes at most once per MinInterval (+ jitter).
func TestRateLimitBound(t *testing.T) {
	t0 := time.Date(2026, 10, 2, 5, 0, 0, 0, time.UTC)
	var st State
	checks := 0
	now := t0
	for i := 0; i < 600; i++ { // one activity every 500 ms for 5 minutes
		now = t0.Add(time.Duration(i) * 500 * time.Millisecond)
		if !st.Scheduled.IsZero() && !now.Before(st.Scheduled) {
			d := Decide(Config{}, st, TriggerScheduled, now, 0.3)
			if d.Action == ActionCheckNow {
				checks++
				st.LastCheck, st.Scheduled = now, time.Time{}
			}
		}
		d := Decide(Config{}, st, TriggerActivity, now, 0.3)
		switch d.Action {
		case ActionCheckNow:
			checks++
			st.LastCheck, st.Scheduled = now, time.Time{}
		case ActionSchedule:
			st.Scheduled = d.At
		}
		st.LastActivity = now
	}
	if limit := int((5*time.Minute)/DefaultMinInterval) + 1; checks > limit || checks < limit-2 {
		t.Fatalf("%d checks in 5 min of dense activity, want about %d (<= %d)", checks, limit-1, limit)
	}
}

// TestHeldProbeVolume pins the T-474 volume claim both ways under dense
// activity: against a server that HOLDS (each probe lasts DefaultHold unless a
// change ends it) an active node keeps one probe open and starts about one per
// hold, no more than the 30 s floor allowed; against a server that claims to
// hold but answers at once, the node is bounded to one probe per HoldGap.
func TestHeldProbeVolume(t *testing.T) {
	t0 := time.Date(2026, 10, 2, 5, 0, 0, 0, time.UTC)
	run := func(answerAfter time.Duration) int {
		st := State{HoldSupported: true}
		starts := 0
		var doneAt time.Time
		start := func(now time.Time) {
			starts++
			st.LastCheck, st.InFlight, doneAt = now, true, now.Add(answerAfter)
		}
		step := 100 * time.Millisecond
		for now := t0; now.Before(t0.Add(5 * time.Minute)); now = now.Add(step) {
			if st.InFlight && !now.Before(doneAt) {
				st.InFlight = false
				d := Decide(Config{}, st, TriggerProbeDone, now, 0.3)
				switch d.Action {
				case ActionCheckNow:
					start(now)
				case ActionSchedule:
					st.Scheduled = d.At
				}
			}
			if !st.Scheduled.IsZero() && !now.Before(st.Scheduled) {
				st.Scheduled = time.Time{}
				if d := Decide(Config{}, st, TriggerScheduled, now, 0.3); d.Action == ActionCheckNow {
					start(now)
				}
			}
			if now.Sub(t0)%(500*time.Millisecond) == 0 { // activity every 500 ms
				d := Decide(Config{}, st, TriggerActivity, now, 0.3)
				switch d.Action {
				case ActionCheckNow:
					start(now)
				case ActionSchedule:
					st.Scheduled = d.At
				}
				st.LastActivity = now
			}
		}
		return starts
	}
	if got, limit := run(DefaultHold), int((5*time.Minute)/DefaultHold)+1; got > limit || got < limit-2 {
		t.Fatalf("holding server: %d probes in 5 min of activity, want about %d", got, limit)
	}
	if got, limit := run(50*time.Millisecond), int((5*time.Minute)/DefaultHoldGap)+1; got > limit {
		t.Fatalf("server answering at once: %d probes in 5 min, want at most %d (one per HoldGap)", got, limit)
	}
}

func TestBackstopDelayAndChanged(t *testing.T) {
	if d := BackstopDelay(5*time.Minute, 0); d != 4*time.Minute {
		t.Fatalf("low jitter = %v", d)
	}
	if d := BackstopDelay(5*time.Minute, 1); d != 6*time.Minute {
		t.Fatalf("high jitter = %v", d)
	}
	if d := BackstopDelay(0, 0.5); d != DefaultBackstop {
		t.Fatalf("default = %v", d)
	}
	if got := Changed(nil, map[string]int64{"budget": 5}); got != nil {
		t.Fatalf("first observation changed = %v", got)
	}
	got := Changed(map[string]int64{"budget": 5, "pricing": 7}, map[string]int64{"budget": 6, "pricing": 7, "bundle": 1})
	sort.Strings(got)
	if strings.Join(got, ",") != "budget,bundle" {
		t.Fatalf("changed = %v", got)
	}
	if got := Changed(map[string]int64{"budget": 6}, map[string]int64{"budget": 5}); len(got) != 0 {
		t.Fatalf("a lower version is not a change (replay): %v", got)
	}
}

// TestPurity pins the freshness table pure: it imports "time" and nothing else.
func TestPurity(t *testing.T) {
	importpin.Check(t, importpin.Spec{
		AllowOnly: true,
		Allowed:   []string{"time"},
		Reason:    "the freshness table is pure (time only)",
	})
}
