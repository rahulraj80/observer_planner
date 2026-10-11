package main

import (
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/marmutapp/superbased-observer/internal/budgetwarnfile"
	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/orgbudget"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

// The hook-path budget admission (T-519, D-332): the daemon half publishes the
// managed budget decision, the hook half reads it. See
// internal/budgetwarnfile/admission.go for why it is a file and
// internal/guard/hookbudget.go for how a stop becomes a verdict.

// hookBudgetChecker is the slice of *guard.Guard the publisher needs: the ONE
// managed budget decision (the same call the process controller and the launch
// gate make) and the developer-facing rendering of its reason.
type hookBudgetChecker interface {
	CheckInterventionBudget(guard.InterventionBudgetInput) guard.InterventionBudgetDecision
	RenderBudgetReason(ruleID, reason string) string
}

// processHookBudgetAdmission returns the publisher over the daemon's shared
// guard for dbPath. No guard in this process publishes no stops (the hook
// guard is built from the same config, so it is off too); the decision is
// otherwise always available, because CheckInterventionBudget folds its own
// failures into a fail-closed deny rather than an error.
func processHookBudgetAdmission(dbPath string) func(time.Time, []orgbudget.SubjectCap) ([]budgetwarnfile.Stop, bool) {
	grace := newHookBudgetGrace()
	return func(now time.Time, subjects []orgbudget.SubjectCap) ([]budgetwarnfile.Stop, bool) {
		g := lookupProcessGuard(dbPath)
		if g == nil {
			return nil, true
		}
		return hookBudgetStopsGraced(g, grace, subjects, now), true
	}
}

// hookBudgetGraceFailures is how many CONSECUTIVE ticks a subject's spend read
// must fail before its fail-closed stop is published (D-359, review P3-11).
// One overrunning read used to become a node-wide B-625 at once, refusing
// every hook-capable tool although nothing was over a cap. Until the bound,
// the subject keeps the decision its last successful read took. Only a read
// failure gets this grace: a missing price table, a broken capture source or a
// policy that cannot evaluate still stop at once.
const hookBudgetGraceFailures = 3

// hookBudgetSnapshotAge is how old a good spend snapshot the publisher may
// reuse (review P3-11): the node-wide decision and every hard per-tool cap
// share one read, and consecutive ticks share it too, instead of 1+N reads
// every budgetAdmissionTick.
const hookBudgetSnapshotAge = 3 * budgetAdmissionTick

// hookBudgetGrace is the publisher's memory across ticks: per subject (""
// for node-wide, else the normalized tool), the consecutive failed reads and
// the stop the last successful read published (absent = no stop). One owner:
// the closure processHookBudgetAdmission returns.
type hookBudgetGrace struct {
	mu    sync.Mutex
	fails map[string]int
	last  map[string]budgetwarnfile.Stop
}

func newHookBudgetGrace() *hookBudgetGrace {
	return &hookBudgetGrace{fails: map[string]int{}, last: map[string]budgetwarnfile.Stop{}}
}

// settle turns one decision into the stop to publish for subject.
func (h *hookBudgetGrace) settle(subject string, d guard.InterventionBudgetDecision, render func() (budgetwarnfile.Stop, bool)) (budgetwarnfile.Stop, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if d.Deny && d.AccountingUnavailable {
		h.fails[subject]++
		if h.fails[subject] < hookBudgetGraceFailures {
			s, had := h.last[subject]
			return s, had
		}
		return render()
	}
	delete(h.fails, subject)
	s, ok := render()
	if ok {
		h.last[subject] = s
	} else {
		delete(h.last, subject)
	}
	return s, ok
}

// hookBudgetStops takes the node-wide decision and, when it admits, one
// decision per tool the organization capped HARD (B-626/B-627: a per-tool cap
// stops only that tool). Per-MODEL caps are not published: a hook names its
// tool but, on most seams, not the model the next turn will use, and a stop on
// a guessed model is worse than one that honestly bites only on the proxy and
// the running-session pass (docs/budgets.md, the subject caveat).
//
// SourceReady is true, the launch gate's rule for a tool with no controller
// report: the hook path judges spend the store already measured; a tool whose
// capture source is structurally broken is the process controller's to stop.
func hookBudgetStops(g hookBudgetChecker, subjects []orgbudget.SubjectCap, now time.Time) []budgetwarnfile.Stop {
	return hookBudgetStopsGraced(g, nil, subjects, now)
}

// hookBudgetStopsGraced is hookBudgetStops with the publisher's cross-tick
// grace (nil = none: every decision is published as taken).
func hookBudgetStopsGraced(g hookBudgetChecker, grace *hookBudgetGrace, subjects []orgbudget.SubjectCap, now time.Time) []budgetwarnfile.Stop {
	if s, ok := hookBudgetStop(g, grace, "", now); ok {
		// A node-wide stop covers every tool; per-tool stops would only repeat it.
		return []budgetwarnfile.Stop{s}
	}
	seen := map[string]bool{}
	var out []budgetwarnfile.Stop
	for _, sc := range subjects {
		tool := budgetwarnfile.NormalizeTool(sc.ID)
		// Hard is the cap's RESOLVED hardness (the windowHard rule the guard's
		// own subject rows read), not the org's authored mode string.
		if sc.Kind != orgcontract.BudgetSubjectTool || !sc.Hard || tool == "" || seen[tool] {
			continue
		}
		seen[tool] = true
		if s, ok := hookBudgetStop(g, grace, tool, now); ok {
			s.Tool = tool
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tool < out[j].Tool })
	return out
}

// hookBudgetStop is one CheckInterventionBudget call rendered as a stop. A deny
// with no rule (the guard could not evaluate, or a managed guard is not
// enforcing) is published under B-625, the managed fail-closed row, exactly as
// the launch gate names a missing guard.
func hookBudgetStop(g hookBudgetChecker, grace *hookBudgetGrace, tool string, now time.Time) (budgetwarnfile.Stop, bool) {
	d := g.CheckInterventionBudget(guard.InterventionBudgetInput{
		Tool: tool, SourceReady: true, Now: now, MaxSnapshotAge: hookBudgetSnapshotAge,
	})
	render := func() (budgetwarnfile.Stop, bool) {
		if !d.Deny {
			return budgetwarnfile.Stop{}, false
		}
		rule := d.RuleID
		if rule == "" {
			rule = "B-625"
		}
		return budgetwarnfile.Stop{RuleID: rule, Reason: g.RenderBudgetReason(rule, d.Reason), ValidUntil: d.ValidUntil.UTC()}, true
	}
	if grace == nil {
		return render()
	}
	return grace.settle(tool, d, render)
}

// hookBudgetAdmissionLookup is the hook half: one small file read per judged
// event, no database, no network (the budget-notice precedent).
//
// FAIL OPEN on the TRANSPORT, never on the decision. A missing file means no
// stop was ever published here (an individual node, a daemon that predates
// this file). A malformed one is said on stderr and stops nothing: the managed
// fail-closed posture is a DECISION the daemon publishes (B-625, unavailable
// accounting), and the proxy, the launch gate and the process controller keep
// measuring first-hand whatever this file says.
//
// A STALE file stops nothing either (D-359, review P3-05): the daemon re-writes
// a file that carries a stop every budgetwarnfile.AdmissionRefresh, so one
// older than budgetwarnfile.AdmissionMaxAge has no live publisher (the daemon
// is down, or the node left its organization) and is said on stderr.
func hookBudgetAdmissionLookup(dbPath string, stderr io.Writer) guard.HookBudgetLookup {
	path := budgetwarnfile.AdmissionPath(dbPath)
	if path == "" {
		return nil
	}
	return func(tool string, now time.Time) (guard.HookBudgetStop, bool) {
		a, err := budgetwarnfile.ReadAdmission(path)
		if err != nil {
			if stderr != nil {
				fmt.Fprintf(stderr, "observer-hook: budget admission unreadable (no budget stop applied): %v\n", err)
			}
			return guard.HookBudgetStop{}, false
		}
		if a != nil && len(a.Stops) > 0 && a.Stale(now) {
			if stderr != nil {
				fmt.Fprintf(stderr, "observer-hook: budget admission is stale (written %s, no live publisher; no budget stop applied)\n",
					a.WrittenAt.UTC().Format(time.RFC3339))
			}
			return guard.HookBudgetStop{}, false
		}
		s, ok := a.StopFor(tool, now)
		if !ok {
			return guard.HookBudgetStop{}, false
		}
		return guard.HookBudgetStop{RuleID: s.RuleID, Reason: s.Reason}, true
	}
}
