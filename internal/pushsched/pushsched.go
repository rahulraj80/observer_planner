package pushsched

import "time"

// Defaults for the activity-driven push (operator decision 2026-10-02).
const (
	DefaultFloor      = 15 * time.Second
	DefaultQuiet      = 3 * time.Second
	DefaultMaxWait    = 10 * time.Second
	DefaultBacklogGap = time.Second
	// MaxServerMin caps the org's backpressure hint so a bad value can never
	// park a node for longer than an hour.
	MaxServerMin = time.Hour
)

// Config bounds the schedule. Zero fields take the defaults (Ceiling has
// none: the caller always passes the effective push interval).
type Config struct {
	Floor      time.Duration
	Quiet      time.Duration
	MaxWait    time.Duration
	BacklogGap time.Duration
	Ceiling    time.Duration
	// ServerMin is the org's X-SBO-Push-Min-Interval hint (0 = none). It
	// raises the floor and the backlog gap; it never lowers anything.
	ServerMin time.Duration
}

// State is what the loop remembers between pushes.
type State struct {
	// LastPush is when the last push cycle started (any kind); zero before
	// the first.
	LastPush time.Time
	// LastRails is when the last rail-refreshing (ceiling) cycle started;
	// zero before the first, which makes the first cycle immediate.
	LastRails time.Time
	// FirstPending / LastPending bound the activity not yet pushed; zero
	// FirstPending means none.
	FirstPending time.Time
	LastPending  time.Time
	// Backlog is true when the last push was cut by the byte budget.
	Backlog bool
}

// Decision is when to run the next cycle and what kind it is.
type Decision struct {
	At    time.Time
	Rails bool
	// RailsOnly marks a ceiling cycle that falls inside the org's push
	// backpressure window (ServerMin after the last push): the governance
	// rails are refreshed on time, but no rows are uploaded (Sol A5-06). The
	// X-SBO-Push-Min-Interval contract slows uploads only; it never delays
	// the ceiling. Always false when Rails is false.
	RailsOnly bool
	Rule      string
}

// Rule names (one table row each).
const (
	RuleFirst    = "first_cycle"
	RuleCeiling  = "ceiling"
	RuleBacklog  = "backlog"
	RuleActivity = "activity"
)

type rule struct {
	name  string
	rails bool
	at    func(c Config, s State) (time.Time, bool)
}

// rules is the ordered table. RuleFirst short-circuits; the others each
// propose a time and the earliest wins (ties to the earlier row).
var rules = []rule{
	{RuleFirst, true, func(_ Config, s State) (time.Time, bool) {
		return time.Time{}, s.LastRails.IsZero()
	}},
	{RuleCeiling, true, func(c Config, s State) (time.Time, bool) {
		// The ceiling is LastRails + Ceiling and nothing else (Sol A5-06):
		// the org's backpressure hint must not push a rail refresh an hour
		// out. Only an overdue ceiling is held to the last push, so it never
		// schedules in the past; ServerMin decides RailsOnly in Next.
		return later(s.LastRails.Add(c.Ceiling), s.LastPush), true
	}},
	{RuleBacklog, false, func(c Config, s State) (time.Time, bool) {
		if !s.Backlog {
			return time.Time{}, false
		}
		return s.LastPush.Add(maxDur(c.BacklogGap, c.ServerMin)), true
	}},
	{RuleActivity, false, func(c Config, s State) (time.Time, bool) {
		if s.FirstPending.IsZero() {
			return time.Time{}, false
		}
		last := s.LastPending
		if last.Before(s.FirstPending) {
			last = s.FirstPending
		}
		at := earlier(last.Add(c.Quiet), s.FirstPending.Add(c.MaxWait))
		return later(at, s.LastPush.Add(maxDur(c.Floor, c.ServerMin))), true
	}},
}

// Next returns the next cycle.
func Next(cfg Config, st State) Decision {
	cfg = withDefaults(cfg)
	var best Decision
	found := false
	for _, r := range rules {
		at, ok := r.at(cfg, st)
		if !ok {
			continue
		}
		if r.name == RuleFirst {
			return Decision{At: at, Rails: true, Rule: r.name}
		}
		if !found || at.Before(best.At) {
			best, found = Decision{At: at, Rails: r.rails, Rule: r.name}, true
		}
	}
	// A rail cycle inside the backpressure window refreshes the rails and
	// leaves the upload to a later cycle (the activity/backlog rules already
	// honour ServerMin).
	best.RailsOnly = best.Rails && cfg.ServerMin > 0 && !st.LastPush.IsZero() &&
		best.At.Before(st.LastPush.Add(cfg.ServerMin))
	return best
}

// ClampServerMin bounds the org's hint.
func ClampServerMin(d time.Duration) time.Duration {
	switch {
	case d < 0:
		return 0
	case d > MaxServerMin:
		return MaxServerMin
	default:
		return d
	}
}

func withDefaults(c Config) Config {
	if c.Floor <= 0 {
		c.Floor = DefaultFloor
	}
	if c.Quiet <= 0 {
		c.Quiet = DefaultQuiet
	}
	if c.MaxWait <= 0 {
		c.MaxWait = DefaultMaxWait
	}
	if c.BacklogGap <= 0 {
		c.BacklogGap = DefaultBacklogGap
	}
	if c.Ceiling <= 0 {
		c.Ceiling = 2 * time.Minute
	}
	c.ServerMin = ClampServerMin(c.ServerMin)
	return c
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxDur(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
