package planlimits

import (
	"errors"
	"math"
	"sort"
	"time"
)

// rules.go holds the module's decision logic as ordered tables walked
// top-down (CLAUDE.md rule 5): the ladder, the poll cadence, the backoff, the
// suspicious-reset hold, merge and pace.

// --- The ladder ---------------------------------------------------------------

// OfficialFreshFor is how recent a rung-1 reading must be for the ladder to
// skip a rung-2 source of the same provider.
const OfficialFreshFor = time.Hour

// LadderInput is what the ladder rule reads.
type LadderInput struct {
	Source          SourceDef
	OptedIn         bool      // the provider's experimental switch
	OfficialFreshAt time.Time // newest rung-1 reading of the provider, zero when none
	Now             time.Time
}

type ladderRule struct {
	reason string
	match  func(LadderInput) bool
	poll   bool
}

// ladderRules: rung 2 runs only when the user opted in for the provider AND
// rung 1 gives no fresh coverage (D-640: "Option 1 wherever possible").
var ladderRules = []ladderRule{
	{StatusNotOptedIn, func(in LadderInput) bool { return in.Source.Experimental() && !in.OptedIn }, false},
	{StatusSkipped, func(in LadderInput) bool {
		return in.Source.Experimental() && !in.OfficialFreshAt.IsZero() && in.Now.Sub(in.OfficialFreshAt) <= OfficialFreshFor
	}, false},
	{"", func(LadderInput) bool { return true }, true},
}

// ShouldPoll reports whether a source may poll now, and the reason code when
// it may not.
func ShouldPoll(in LadderInput) (bool, string) {
	for _, r := range ladderRules {
		if r.match(in) {
			return r.poll, r.reason
		}
	}
	return false, StatusSkipped
}

// --- Cadence ------------------------------------------------------------------

// CadenceInput is what the cadence rule reads.
type CadenceInput struct {
	MinInterval time.Duration
	PageOpen    bool // the Plans & limits page was open in the last 5 minutes
	NearLimit   bool // a window is within 10% of its limit or resets within 15 minutes
}

type cadenceRule struct {
	match func(CadenceInput) bool
	every time.Duration
}

var cadenceRules = []cadenceRule{
	{func(in CadenceInput) bool { return in.PageOpen }, 2 * time.Minute},
	{func(in CadenceInput) bool { return in.NearLimit }, 5 * time.Minute},
	{func(CadenceInput) bool { return true }, 30 * time.Minute},
}

// Cadence returns the poll interval, never below the source's floor.
func Cadence(in CadenceInput) time.Duration {
	for _, r := range cadenceRules {
		if r.match(in) {
			return max(r.every, in.MinInterval)
		}
	}
	return max(30*time.Minute, in.MinInterval)
}

// NearLimit reports whether any window of s is within 10% of its limit or
// resets within 15 minutes.
func NearLimit(s Snapshot, now time.Time) bool {
	for _, w := range s.Windows {
		if f, ok := usedFrac(w); ok && f >= 0.9 {
			return true
		}
		if w.ResetsAt != nil && w.ResetsAt.After(now) && w.ResetsAt.Sub(now) <= 15*time.Minute {
			return true
		}
	}
	return false
}

// --- Backoff ------------------------------------------------------------------

// RunState is the polling state of one account.
type RunState struct {
	Status        string    `json:"status"`
	Failures      int       `json:"failures"`
	ShapeFailures int       `json:"shape_failures"`
	NextDue       time.Time `json:"next_due"`
	Stopped       bool      `json:"stopped"` // polling paused until the user acts
}

// Outcome is the result of one refresh.
type Outcome struct {
	Err        error
	RetryAfter time.Duration
}

// maxShapeFailures stops polling after this many consecutive shape errors.
const maxShapeFailures = 3

// backoffStep returns 2, 4, 8 ... 60 minutes for the nth consecutive failure.
func backoffStep(n int) time.Duration {
	if n < 1 {
		n = 1
	}
	d := time.Duration(math.Pow(2, float64(min(n, 6)))) * time.Minute
	return min(d, time.Hour)
}

type backoffRule struct {
	target error // nil matches success
	apply  func(prev RunState, o Outcome, now time.Time, cadence time.Duration) RunState
}

// backoffRules classify an outcome by its sentinel error, first match wins.
var backoffRules = []backoffRule{
	{nil, func(_ RunState, _ Outcome, now time.Time, cadence time.Duration) RunState {
		return RunState{Status: StatusOK, NextDue: now.Add(cadence)}
	}},
	{ErrAuth, func(p RunState, _ Outcome, now time.Time, _ time.Duration) RunState {
		return RunState{Status: StatusAuthNeeded, Failures: p.Failures + 1, Stopped: true, NextDue: now}
	}},
	{ErrRateLimited, func(p RunState, o Outcome, now time.Time, _ time.Duration) RunState {
		n := p.Failures + 1
		wait := backoffStep(n)
		if o.RetryAfter > 0 {
			wait = min(o.RetryAfter, time.Hour)
		}
		return RunState{Status: StatusThrottled, Failures: n, NextDue: now.Add(wait)}
	}},
	{ErrShape, shapeBackoff},
	{ErrGone, shapeBackoff},
	{ErrNotApplicable, func(p RunState, _ Outcome, now time.Time, _ time.Duration) RunState {
		return RunState{Status: "not_applicable", Failures: p.Failures + 1, NextDue: now.Add(6 * time.Hour)}
	}},
	{ErrUnavailable, func(p RunState, _ Outcome, now time.Time, _ time.Duration) RunState {
		return RunState{Status: "unavailable", Failures: p.Failures + 1, NextDue: now.Add(time.Hour)}
	}},
}

func shapeBackoff(p RunState, _ Outcome, now time.Time, _ time.Duration) RunState {
	n := p.ShapeFailures + 1
	return RunState{
		Status: StatusEndpointGone, Failures: p.Failures + 1, ShapeFailures: n,
		Stopped: n >= maxShapeFailures, NextDue: now.Add(backoffStep(p.Failures + 1)),
	}
}

// NextRun folds one outcome into the account's run state. An error that
// matches no row is transient: keep the last good reading, back off.
func NextRun(prev RunState, o Outcome, now time.Time, cadence time.Duration) RunState {
	for _, r := range backoffRules {
		if (r.target == nil && o.Err == nil) || (r.target != nil && o.Err != nil && errors.Is(o.Err, r.target)) {
			return r.apply(prev, o, now, cadence)
		}
	}
	n := prev.Failures + 1
	return RunState{Status: StatusError, Failures: n, ShapeFailures: prev.ShapeFailures, NextDue: now.Add(backoffStep(n))}
}

// OutcomeCode is the stable code stored for a refresh outcome.
func OutcomeCode(err error) string {
	return NextRun(RunState{}, Outcome{Err: err}, time.Time{}, 0).Status
}

// --- Suspicious resets ----------------------------------------------------------

// suspiciousDrop is how far a window's used fraction must fall, before its
// stated reset, for a reading to be held back for confirmation.
const suspiciousDrop = 0.25

// Suspicious reports whether next looks like a reset that has not happened:
// a window falls sharply while the previous reading said it resets later.
func Suspicious(prev, next Snapshot, now time.Time) bool {
	for _, nw := range next.Windows {
		pw, ok := FindWindow(prev, nw.Key)
		if !ok || pw.ResetsAt == nil || !pw.ResetsAt.After(now.Add(time.Minute)) {
			continue
		}
		pf, ok1 := usedFrac(pw)
		nf, ok2 := usedFrac(nw)
		if ok1 && ok2 && nf < pf-suspiciousDrop {
			return true
		}
	}
	return false
}

// Agrees reports whether two readings say the same thing (every shared window
// within 5 points), so a held reading is confirmed by a second one.
func Agrees(a, b Snapshot) bool {
	for _, w := range b.Windows {
		x, ok := FindWindow(a, w.Key)
		if !ok {
			continue
		}
		f1, ok1 := usedFrac(x)
		f2, ok2 := usedFrac(w)
		if ok1 && ok2 && math.Abs(f1-f2) > 0.05 {
			return false
		}
	}
	return true
}

// --- Merge --------------------------------------------------------------------

// Provenance says where one number came from.
type Provenance struct {
	Source     string     `json:"source"`
	Kind       SourceKind `json:"source_kind"`
	Rung       Rung       `json:"rung"`
	Confidence Confidence `json:"confidence"`
	ObservedAt time.Time  `json:"observed_at"`
}

var confRank = map[Confidence]int{ConfExact: 3, ConfPercentOnly: 2, ConfEstimated: 1, ConfUnknown: 0}

func provOf(s Snapshot) Provenance {
	return Provenance{Source: s.Source, Kind: s.Kind, Rung: s.Rung, Confidence: s.Confidence, ObservedAt: s.ObservedAt}
}

// better reports whether candidate c should replace current cur for one
// window key: a manual reading never displaces a measured one; otherwise the
// freshest wins; on a tie the higher confidence wins.
func better(c, cur Provenance) bool {
	switch {
	case c.Kind == KindManual && cur.Kind != KindManual:
		return false
	case cur.Kind == KindManual && c.Kind != KindManual:
		return true
	case c.ObservedAt.After(cur.ObservedAt):
		return true
	case c.ObservedAt.Equal(cur.ObservedAt):
		return confRank[c.Confidence] > confRank[cur.Confidence]
	}
	return false
}

// Merged is a merged view of several snapshots of one account.
type Merged struct {
	Snapshot
	WindowProv  map[string]Provenance
	BalanceProv map[string]Provenance
}

// Merge folds snapshots of ONE account into one view, per window key, per
// balance (kind, unit) and per spend (period, unit, scope).
func Merge(snaps []Snapshot) Merged {
	out := Merged{WindowProv: map[string]Provenance{}, BalanceProv: map[string]Provenance{}}
	type wpick struct {
		w Window
		p Provenance
	}
	type bpick struct {
		b Balance
		p Provenance
	}
	type spick struct {
		s Spend
		p Provenance
	}
	ws, bs, ss := map[string]wpick{}, map[string]bpick{}, map[string]spick{}
	var newest time.Time
	planAt := time.Time{}
	for _, s := range snaps {
		p := provOf(s)
		if out.Provider == "" {
			out.Provider = s.Provider
		}
		if s.Kind != KindManual && (newest.IsZero() || s.ObservedAt.After(newest)) {
			newest = s.ObservedAt
			out.Source, out.Kind, out.Rung, out.Confidence, out.Status = s.Source, s.Kind, s.Rung, s.Confidence, s.Status
		}
		if (s.PlanCode != "" || s.PlanLabel != "") && (planAt.IsZero() || (s.Kind != KindManual && s.ObservedAt.After(planAt))) {
			planAt = s.ObservedAt
			out.PlanCode, out.PlanLabel = s.PlanCode, s.PlanLabel
		}
		for _, w := range s.Windows {
			if cur, ok := ws[w.Key]; !ok || better(p, cur.p) {
				ws[w.Key] = wpick{w, p}
			}
		}
		for _, b := range s.Balances {
			k := b.Kind + "|" + string(b.Unit)
			if cur, ok := bs[k]; !ok || better(p, cur.p) {
				bs[k] = bpick{b, p}
			}
		}
		for _, sp := range s.Spend {
			k := sp.Period + "|" + string(sp.Unit) + "|" + sp.Scope
			if cur, ok := ss[k]; !ok || better(p, cur.p) {
				ss[k] = spick{sp, p}
			}
		}
	}
	if newest.IsZero() && len(snaps) > 0 { // manual only
		s := snaps[0]
		newest = s.ObservedAt
		out.Source, out.Kind, out.Rung, out.Confidence, out.Status = s.Source, s.Kind, s.Rung, s.Confidence, s.Status
	}
	out.ObservedAt = newest
	for _, k := range sortedKeys(ws) {
		out.Windows = append(out.Windows, ws[k].w)
		out.WindowProv[k] = ws[k].p
	}
	for _, k := range sortedKeys(bs) {
		out.Balances = append(out.Balances, bs[k].b)
		out.BalanceProv[k] = bs[k].p
	}
	for _, k := range sortedKeys(ss) {
		out.Spend = append(out.Spend, ss[k].s)
	}
	return out
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// --- Pace -----------------------------------------------------------------------

// Pace is a projection of a window to its reset at the current rate.
type Pace struct {
	ProjectedFrac float64    `json:"projected_frac"`
	RunsOutAt     *time.Time `json:"runs_out_at,omitempty"`
}

// PaceOf projects a window linearly from its start to its reset. nil when the
// window has no fraction, reset or length, or is too young to project.
func PaceOf(w Window, now time.Time) *Pace {
	f, ok := usedFrac(w)
	if !ok || f <= 0 || w.ResetsAt == nil || w.Length == nil || *w.Length <= 0 || !w.ResetsAt.After(now) {
		return nil
	}
	left := w.ResetsAt.Sub(now)
	elapsed := *w.Length - left
	if elapsed < *w.Length/20 {
		return nil
	}
	rate := f / float64(elapsed)
	p := &Pace{ProjectedFrac: f + rate*float64(left)}
	if p.ProjectedFrac > 1 && f < 1 {
		t := now.Add(time.Duration((1 - f) / rate))
		p.RunsOutAt = &t
	}
	return p
}

// Expired reports whether a window's stated reset has passed: the reading no
// longer describes the current window, so its use is unknown (never zero).
func Expired(w Window, now time.Time) bool {
	return w.ResetsAt != nil && !w.ResetsAt.After(now)
}
