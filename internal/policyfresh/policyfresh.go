package policyfresh

import "time"

// Trigger is what woke the checker.
type Trigger int

const (
	// TriggerActivity is node activity (a proxied request, a prompt hook).
	TriggerActivity Trigger = iota
	// TriggerScheduled is a check this table scheduled earlier (rule 4).
	TriggerScheduled
	// TriggerBackstop is the periodic no-activity check.
	TriggerBackstop
	// TriggerProbeDone is a probe that just finished. The caller records its
	// outcome in State (LastProbeFailed, HoldSupported) before asking.
	TriggerProbeDone
)

// Defaults (operator decision 4, 2026-10-02; the held probe, T-474).
const (
	DefaultMinInterval   = 30 * time.Second
	DefaultMaxJitter     = 5 * time.Second
	DefaultIdleThreshold = 5 * time.Minute
	DefaultBackstop      = 5 * time.Minute
	// DefaultUnsupportedBackoff is how long a 404 ("this server has no
	// probe") silences the checker before it asks again.
	DefaultUnsupportedBackoff = time.Hour
	// DefaultHold is how long an ACTIVE node asks the org server to hold a
	// probe open while nothing changes (T-474). The server answers the moment
	// a version moves, so a change reaches an active node within seconds while
	// the node still sends about one request per hold, the same volume as the
	// 30 s activity floor. It stays well under the node's 30 s HTTP client
	// timeout and the org server's 60 s write timeout.
	DefaultHold = 20 * time.Second
	// DefaultHoldGap is the minimum time between the STARTS of two held
	// probes. A server that holds honours it by construction (each probe
	// lasts the hold); it bounds a server that answers early, or reports a
	// change on every answer, to one probe per gap.
	DefaultHoldGap = 3 * time.Second
)

// Config tunes the table; zero fields take the defaults.
type Config struct {
	MinInterval   time.Duration
	MaxJitter     time.Duration
	IdleThreshold time.Duration
	HoldGap       time.Duration
}

func (c Config) withDefaults() Config {
	if c.MinInterval <= 0 {
		c.MinInterval = DefaultMinInterval
	}
	if c.MaxJitter < 0 {
		c.MaxJitter = 0
	} else if c.MaxJitter == 0 {
		c.MaxJitter = DefaultMaxJitter
	}
	if c.IdleThreshold <= 0 {
		c.IdleThreshold = DefaultIdleThreshold
	}
	if c.HoldGap <= 0 {
		c.HoldGap = DefaultHoldGap
	}
	return c
}

// State is the checker's memory the caller keeps between calls.
type State struct {
	// LastCheck is when the last probe STARTED (zero = never).
	LastCheck time.Time
	// LastActivity is the previous activity event (zero = never).
	LastActivity time.Time
	// InFlight is true while a probe runs.
	InFlight bool
	// Scheduled is the pending scheduled check (zero = none).
	Scheduled time.Time
	// UnsupportedUntil silences the checker after a 404.
	UnsupportedUntil time.Time
	// HoldSupported is true when the last probe answer said the server held
	// it (orgcontract.HeaderPolicyVersionsHold). An older server never says
	// so, and the node then keeps the plain activity cadence.
	HoldSupported bool
	// LastProbeFailed is true when the last probe ended in a transport or
	// server error. A failed probe is never re-armed at once: the node falls
	// back to the activity cadence (MinInterval) until a probe succeeds.
	LastProbeFailed bool
}

// Action is the decision kind.
type Action int

const (
	// ActionNone does nothing.
	ActionNone Action = iota
	// ActionCheckNow runs a probe now.
	ActionCheckNow
	// ActionSchedule arranges a probe at Decision.At (unless one is already
	// scheduled at or before it).
	ActionSchedule
)

// Decision is the table's answer. Rule names the row that decided (for tests
// and debug logs). Hold asks the caller to send the probe as a HELD probe (the
// node is active); a backstop probe of an idle node never holds.
type Decision struct {
	Action Action
	At     time.Time
	Hold   bool
	Rule   string
}

// Decide walks the table. jitter is the caller's draw in [0, 1).
func Decide(cfg Config, st State, trig Trigger, now time.Time, jitter float64) Decision {
	cfg = cfg.withDefaults()
	for _, r := range rules {
		if d, ok := r.apply(cfg, st, trig, now, jitter); ok {
			d.Rule = r.name
			return d
		}
	}
	return Decision{Action: ActionCheckNow, Hold: true, Rule: "default"}
}

// active reports whether the node has seen activity within the idle
// threshold: the one definition of "active" the hold re-arm and the
// first-after-idle rule share.
func active(cfg Config, st State, now time.Time) bool {
	return !st.LastActivity.IsZero() && now.Sub(st.LastActivity) < cfg.IdleThreshold
}

type rule struct {
	name  string
	apply func(cfg Config, st State, trig Trigger, now time.Time, jitter float64) (Decision, bool)
}

var rules = []rule{
	{"unsupported", func(_ Config, st State, _ Trigger, now time.Time, _ float64) (Decision, bool) {
		return Decision{Action: ActionNone}, now.Before(st.UnsupportedUntil)
	}},
	{"in_flight", func(_ Config, st State, _ Trigger, _ time.Time, _ float64) (Decision, bool) {
		return Decision{Action: ActionNone}, st.InFlight
	}},
	{"probe_done", func(cfg Config, st State, trig Trigger, now time.Time, _ float64) (Decision, bool) {
		if trig != TriggerProbeDone {
			return Decision{}, false
		}
		// Re-arm the held probe only while the node is active, the server
		// holds, and the last probe did not fail; otherwise the plain rules
		// below take over at the next activity.
		if st.LastProbeFailed || !st.HoldSupported || !active(cfg, st, now) {
			return Decision{Action: ActionNone}, true
		}
		at := st.LastCheck.Add(cfg.HoldGap)
		if now.Before(at) {
			return Decision{Action: ActionSchedule, At: at, Hold: true}, true
		}
		return Decision{Action: ActionCheckNow, Hold: true}, true
	}},
	{"backstop_or_scheduled", func(_ Config, _ State, trig Trigger, _ time.Time, _ float64) (Decision, bool) {
		// A scheduled check comes from activity (or a re-arm), so it holds; the
		// backstop is the idle path and never does.
		return Decision{Action: ActionCheckNow, Hold: trig == TriggerScheduled}, trig != TriggerActivity
	}},
	{"already_scheduled", func(_ Config, st State, _ Trigger, _ time.Time, _ float64) (Decision, bool) {
		// A probe is already pending for this burst: activity before it fires
		// joins it instead of adding another.
		return Decision{Action: ActionNone}, !st.Scheduled.IsZero()
	}},
	{"first_after_idle", func(cfg Config, st State, _ Trigger, now time.Time, _ float64) (Decision, bool) {
		idle := !active(cfg, st, now)
		recent := !st.LastCheck.IsZero() && now.Sub(st.LastCheck) < cfg.MinInterval
		// A check that ran moments ago (e.g. the backstop) already answered
		// this activity; do not double it.
		return Decision{Action: ActionCheckNow, Hold: true}, idle && !recent
	}},
	{"min_interval", func(cfg Config, st State, _ Trigger, now time.Time, jitter float64) (Decision, bool) {
		if st.LastCheck.IsZero() || now.Sub(st.LastCheck) >= cfg.MinInterval {
			return Decision{}, false
		}
		if jitter < 0 {
			jitter = 0
		}
		if jitter >= 1 {
			jitter = 0.999
		}
		at := st.LastCheck.Add(cfg.MinInterval).Add(time.Duration(jitter * float64(cfg.MaxJitter)))
		return Decision{Action: ActionSchedule, At: at, Hold: true}, true
	}},
}

// BackstopDelay is the next backstop wait: Backstop +/- 20% by the jitter draw
// in [0, 1), so a fleet started together does not probe in lockstep.
func BackstopDelay(backstop time.Duration, jitter float64) time.Duration {
	if backstop <= 0 {
		backstop = DefaultBackstop
	}
	if jitter < 0 {
		jitter = 0
	}
	if jitter > 1 {
		jitter = 1
	}
	return time.Duration(float64(backstop) * (0.8 + 0.4*jitter))
}

// Changed returns the keys of next whose value moved forward from prev (a key
// absent from prev counts as moved). A nil prev means "first observation":
// nothing has changed relative to a baseline the caller never had, so it
// returns nil and the caller just records next.
func Changed(prev, next map[string]int64) []string {
	if prev == nil {
		return nil
	}
	var out []string
	for k, v := range next {
		if old, ok := prev[k]; !ok || v > old {
			out = append(out, k)
		}
	}
	return out
}
