package pushsched

import (
	"testing"
	"time"
)

func TestNext(t *testing.T) {
	t0 := time.Date(2026, 10, 2, 5, 0, 0, 0, time.UTC)
	at := func(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }
	cfg := Config{Ceiling: 120 * time.Second}
	tests := []struct {
		name      string
		cfg       Config
		st        State
		wantAt    time.Time
		wantRails bool
		wantRule  string
	}{
		{
			"first cycle is immediate and refreshes rails", cfg,
			State{},
			time.Time{},
			true, RuleFirst,
		},
		{
			"idle node waits for the ceiling", cfg,
			State{LastPush: t0, LastRails: t0},
			at(120), true, RuleCeiling,
		},
		{
			"activity coalesces Quiet after the last turn", cfg,
			State{LastPush: t0, LastRails: t0, FirstPending: at(20), LastPending: at(21)},
			at(24), false, RuleActivity,
		},
		{
			"a busy burst is capped at MaxWait after the first turn", cfg,
			State{LastPush: t0, LastRails: t0, FirstPending: at(20), LastPending: at(29)},
			at(30), false, RuleActivity,
		},
		{
			"activity right after a push waits for the floor", cfg,
			State{LastPush: t0, LastRails: t0, FirstPending: at(1), LastPending: at(1)},
			at(15), false, RuleActivity,
		},
		{
			"the server hint raises the floor",
			Config{Ceiling: 120 * time.Second, ServerMin: 40 * time.Second},
			State{LastPush: t0, LastRails: t0, FirstPending: at(1), LastPending: at(1)},
			at(40), false, RuleActivity,
		},
		{
			"activity near the ceiling folds into the rail push", cfg,
			State{LastPush: at(100), LastRails: t0, FirstPending: at(118), LastPending: at(118)},
			at(120), true, RuleCeiling,
		},
		{
			"a full batch pushes again after the backlog gap", cfg,
			State{LastPush: t0, LastRails: t0, Backlog: true},
			at(1), false, RuleBacklog,
		},
		{
			"the server hint also spaces a backlog drain",
			Config{Ceiling: 120 * time.Second, ServerMin: 30 * time.Second},
			State{LastPush: t0, LastRails: t0, Backlog: true},
			at(30), false, RuleBacklog,
		},
		{
			// Sol A5-06: the hint never delays the ceiling; a ceiling inside
			// the backpressure window refreshes the rails without uploading.
			"the hint never delays the ceiling; inside it the cycle is rail-only",
			Config{Ceiling: 120 * time.Second, ServerMin: 30 * time.Second},
			State{LastPush: at(110), LastRails: t0},
			at(120), true, RuleCeiling,
		},
		{
			"a hint above an hour is clamped",
			Config{Ceiling: 2 * time.Hour, ServerMin: 5 * time.Hour},
			State{LastPush: t0, LastRails: t0, Backlog: true},
			at(3600), false, RuleBacklog,
		},
		{
			"a floor above the ceiling never delays the ceiling",
			Config{Ceiling: 10 * time.Second, Floor: 30 * time.Second},
			State{LastPush: t0, LastRails: t0, FirstPending: at(1), LastPending: at(1)},
			at(10), true, RuleCeiling,
		},
		{
			"an overdue ceiling never schedules before the last push", cfg,
			State{LastPush: t0, LastRails: at(-500)},
			t0, true, RuleCeiling,
		},
		{
			"tie between activity and ceiling goes to the rail push", cfg,
			State{LastPush: at(105), LastRails: t0, FirstPending: at(117), LastPending: at(117)},
			at(120), true, RuleCeiling,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := Next(tc.cfg, tc.st)
			if !d.At.Equal(tc.wantAt) || d.Rails != tc.wantRails || d.Rule != tc.wantRule {
				t.Fatalf("Next = {%v rails=%v %s}, want {%v rails=%v %s}",
					d.At.Sub(t0), d.Rails, d.Rule, tc.wantAt.Sub(t0), tc.wantRails, tc.wantRule)
			}
		})
	}
}

// TestActivityPushRate simulates a node with a turn every 2 s for 10 minutes
// and checks the push count stays between the floor bound and the ceiling.
func TestActivityPushRate(t *testing.T) {
	t0 := time.Date(2026, 10, 2, 5, 0, 0, 0, time.UTC)
	cfg := Config{Ceiling: 120 * time.Second}
	st := State{LastPush: t0, LastRails: t0}
	pushes, rails := 0, 0
	nextTurn := t0.Add(2 * time.Second)
	end := t0.Add(10 * time.Minute)
	for now := t0; now.Before(end); now = now.Add(100 * time.Millisecond) {
		if !now.Before(nextTurn) {
			if st.FirstPending.IsZero() {
				st.FirstPending = now
			}
			st.LastPending = now
			nextTurn = nextTurn.Add(2 * time.Second)
		}
		d := Next(cfg, st)
		if now.Before(d.At) {
			continue
		}
		pushes++
		st.LastPush = now
		if d.Rails {
			rails++
			st.LastRails = now
		}
		st.FirstPending, st.LastPending = time.Time{}, time.Time{}
	}
	// 600 s / 15 s floor = 40 pushes at most (plus the boundary).
	if pushes > 41 || pushes < 30 {
		t.Fatalf("pushes = %d over 10 min of steady activity, want ~40 (floor-bound)", pushes)
	}
	if rails < 4 || rails > 6 {
		t.Fatalf("rail pushes = %d, want ~5 (one per 120 s ceiling)", rails)
	}
}

func TestClampServerMin(t *testing.T) {
	for _, tc := range []struct{ in, want time.Duration }{
		{-time.Second, 0}, {0, 0}, {30 * time.Second, 30 * time.Second}, {2 * time.Hour, time.Hour},
	} {
		if got := ClampServerMin(tc.in); got != tc.want {
			t.Errorf("ClampServerMin(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestCeilingIgnoresServerBackpressure (Sol A5-06): the X-SBO-Push-Min-Interval
// contract slows uploads only. A one-hour hint after a recent activity push
// must not move the next rail refresh off LastRails + Ceiling; inside the
// hint the ceiling cycle is rail-only, outside it the ceiling uploads too.
func TestCeilingIgnoresServerBackpressure(t *testing.T) {
	t0 := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	at := func(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }
	tests := []struct {
		name          string
		cfg           Config
		st            State
		wantAt        time.Time
		wantRailsOnly bool
	}{
		{
			"an hour of backpressure after a push at 12:01:50: rail-only at 12:02",
			Config{Ceiling: 120 * time.Second, ServerMin: time.Hour},
			State{LastPush: at(110), LastRails: t0},
			at(120), true,
		},
		{
			"pending activity does not fold into a rail-only ceiling",
			Config{Ceiling: 120 * time.Second, ServerMin: time.Hour},
			State{LastPush: at(110), LastRails: t0, FirstPending: at(115), LastPending: at(115)},
			at(120), true,
		},
		{
			"a ceiling past the backpressure window uploads as before",
			Config{Ceiling: 120 * time.Second, ServerMin: 30 * time.Second},
			State{LastPush: at(50), LastRails: t0},
			at(120), false,
		},
		{
			"no hint: never rail-only",
			Config{Ceiling: 120 * time.Second},
			State{LastPush: at(110), LastRails: t0},
			at(120), false,
		},
		{
			"an overdue ceiling under backpressure runs now, rail-only",
			Config{Ceiling: 120 * time.Second, ServerMin: time.Hour},
			State{LastPush: t0, LastRails: at(-500)},
			t0, true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := Next(tc.cfg, tc.st)
			if !d.At.Equal(tc.wantAt) || !d.Rails || d.Rule != RuleCeiling || d.RailsOnly != tc.wantRailsOnly {
				t.Fatalf("Next = {%v rails=%v railsOnly=%v %s}, want {%v rails=true railsOnly=%v ceiling}",
					d.At.Sub(t0), d.Rails, d.RailsOnly, d.Rule, tc.wantAt.Sub(t0), tc.wantRailsOnly)
			}
		})
	}
}
