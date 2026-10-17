package componentsvc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/marmutapp/superbased-observer/internal/store"
)

// scheduledpurge.go is the scheduled half of the component purge (modular
// packaging plan 6.3 and 3.2.1, T-682): an organization's node.components
// policy may say `on_remove = {data = "purge", purge_after_days = N}`, an
// explicit future authorisation the admin gives at publish time. The node then
// purges a component it no longer runs, but never silently and never early:
//
//   - while the policy blocks a component that has a reviewed purge plan, the
//     node creates ONE scheduled plan whose grace ends N days plus the 7-day
//     grace after it first saw the block, so the plan, its row counts and its
//     due time are visible on the node (Settings > Components, `observer
//     components purge`) for the whole delay;
//   - any change of the policy in force (a new version, the purge withdrawn,
//     the component no longer blocked, the policy gone) cancels the open plan:
//     that is how the admin cancels, and a new plan starts its full clock
//     again (a purge is never brought forward by an unrelated edit);
//   - an eligible plan executes only after the caller's recheck re-resolves the
//     signed policy inside the purge transaction;
//   - a plan that expired unexecuted (the daemon was down through its window)
//     is replaced by a plan with only the 7-day grace, since its due time has
//     passed; a consumed plan for the same policy version is never repeated.
//
// The decision is one ordered rule table over plain inputs (PlanScheduledPurges,
// pure); RunScheduledPurges applies it through the store seam and audits each
// action.

// ScheduledPurgePolicy is the purge authorisation of the node.components
// policy in force: nil when no policy is in force or it keeps data.
type ScheduledPurgePolicy struct {
	// Version identifies the policy in force (a digest of its compiled
	// form); a plan created under another version is cancelled.
	Version string
	// AfterDays is on_remove.purge_after_days (> 0).
	AfterDays int
	// Held is true when the policy is in force only as LAST-KNOWN-GOOD: the
	// current version did not verify or compile (catalog, selector, floor),
	// so the node cannot tell whether the org withdrew the purge. Open plans
	// are held as they are (no create, no cancel, no execute) until a current
	// policy decides (ledger MODPKG-17 (3)): a destructive purge never runs
	// under a superseded authorisation.
	Held bool
}

// ScheduledPurgeInput is everything PlanScheduledPurges decides from.
type ScheduledPurgeInput struct {
	Now time.Time
	// Policy is the purge authorisation in force, nil when none.
	Policy *ScheduledPurgePolicy
	// Blocked are the components the policy currently blocks (state
	// blocked, reason policy_denied or policy_unlisted).
	Blocked []string
	// Purgeable reports whether a component has a reviewed purge plan.
	Purgeable func(component string) bool
	// Plans are the existing scheduled plans, any state.
	Plans []store.ComponentPurgePlan
}

// ScheduledPurgeActionKind is what the runner does.
type ScheduledPurgeActionKind string

// The three scheduled purge actions.
const (
	PurgeActionCreate  ScheduledPurgeActionKind = "create"
	PurgeActionCancel  ScheduledPurgeActionKind = "cancel"
	PurgeActionExecute ScheduledPurgeActionKind = "execute"
)

// ScheduledPurgeAction is one decided step.
type ScheduledPurgeAction struct {
	Kind      ScheduledPurgeActionKind
	Component string
	// PlanID names the plan to cancel or execute.
	PlanID string
	// Grace is the new plan's grace period (create).
	Grace time.Duration
	// PolicyVersion is the version the new plan is created under (create).
	PolicyVersion string
	// Rule names the table row that decided (audit and tests).
	Rule string
}

func isOpenPurge(s store.PurgeState) bool {
	return s == store.PurgeGracePending || s == store.PurgeEligible
}

// scheduledPurgeRow is one row of the ordered rule table, evaluated per
// component with a reviewed purge plan.
type scheduledPurgeRow struct {
	name string
	when func(c purgeCase) bool
	act  func(c purgeCase) []ScheduledPurgeAction
}

// purgeCase is the per-component view a rule reads.
type purgeCase struct {
	component string
	now       time.Time
	policy    *ScheduledPurgePolicy
	blocked   bool
	open      *store.ComponentPurgePlan // the open scheduled plan, if any
	consumed  bool                      // a consumed plan exists under the policy's version
	expired   bool                      // an expired plan exists under the policy's version
}

// authorised reports whether the policy in force authorises purging the
// component under the given version.
func (c purgeCase) authorised(version string) bool {
	return c.policy != nil && c.blocked && c.policy.Version == version
}

var scheduledPurgeRows = []scheduledPurgeRow{
	{
		name: "policy held as last known good: hold everything",
		when: func(c purgeCase) bool { return c.policy != nil && c.policy.Held },
		act:  func(purgeCase) []ScheduledPurgeAction { return nil },
	},
	{
		name: "open plan no longer authorised: cancel",
		when: func(c purgeCase) bool { return c.open != nil && !c.authorised(c.open.PolicyVersion) },
		act: func(c purgeCase) []ScheduledPurgeAction {
			return []ScheduledPurgeAction{{Kind: PurgeActionCancel, Component: c.component, PlanID: c.open.ID}}
		},
	},
	{
		name: "open plan past its grace: execute",
		when: func(c purgeCase) bool {
			return c.open != nil && (c.open.State == store.PurgeEligible ||
				(c.open.GraceUntil != nil && !c.now.Before(*c.open.GraceUntil)))
		},
		act: func(c purgeCase) []ScheduledPurgeAction {
			return []ScheduledPurgeAction{{Kind: PurgeActionExecute, Component: c.component, PlanID: c.open.ID}}
		},
	},
	{
		name: "open plan in grace: wait",
		when: func(c purgeCase) bool { return c.open != nil },
		act:  func(purgeCase) []ScheduledPurgeAction { return nil },
	},
	{
		name: "not authorised: nothing",
		when: func(c purgeCase) bool { return c.policy == nil || !c.blocked },
		act:  func(purgeCase) []ScheduledPurgeAction { return nil },
	},
	{
		name: "already purged under this policy: nothing",
		when: func(c purgeCase) bool { return c.consumed },
		act:  func(purgeCase) []ScheduledPurgeAction { return nil },
	},
	{
		name: "expired unexecuted: reschedule with the grace only",
		when: func(c purgeCase) bool { return c.expired },
		act: func(c purgeCase) []ScheduledPurgeAction {
			return []ScheduledPurgeAction{{Kind: PurgeActionCreate, Component: c.component, Grace: store.ScheduledPurgeGrace, PolicyVersion: c.policy.Version}}
		},
	},
	{
		name: "newly blocked: schedule after the delay plus the grace",
		when: func(purgeCase) bool { return true },
		act: func(c purgeCase) []ScheduledPurgeAction {
			grace := time.Duration(c.policy.AfterDays)*24*time.Hour + store.ScheduledPurgeGrace
			return []ScheduledPurgeAction{{Kind: PurgeActionCreate, Component: c.component, Grace: grace, PolicyVersion: c.policy.Version}}
		},
	},
}

// PlanScheduledPurges decides the scheduled purge actions (pure). Every
// component with a scheduled plan or a policy block is considered; the
// result is sorted by component.
func PlanScheduledPurges(in ScheduledPurgeInput) []ScheduledPurgeAction {
	purgeable := in.Purgeable
	if purgeable == nil {
		purgeable = store.HasPurgePlan
	}
	cases := map[string]*purgeCase{}
	get := func(id string) *purgeCase {
		c, ok := cases[id]
		if !ok {
			c = &purgeCase{component: id, now: in.Now, policy: in.Policy}
			cases[id] = c
		}
		return c
	}
	for _, id := range in.Blocked {
		if purgeable(id) {
			get(id).blocked = true
		}
	}
	for i := range in.Plans {
		p := in.Plans[i]
		c := get(p.Component)
		switch {
		case isOpenPurge(p.State):
			if c.open == nil {
				c.open = &p
			}
		case in.Policy != nil && p.PolicyVersion == in.Policy.Version && p.State == store.PurgeConsumed:
			c.consumed = true
		case in.Policy != nil && p.PolicyVersion == in.Policy.Version && p.State == store.PurgeExpired:
			c.expired = true
		}
	}
	ids := make([]string, 0, len(cases))
	for id := range cases {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []ScheduledPurgeAction
	for _, id := range ids {
		c := *cases[id]
		for _, row := range scheduledPurgeRows {
			if !row.when(c) {
				continue
			}
			for _, a := range row.act(c) {
				a.Rule = row.name
				out = append(out, a)
			}
			break
		}
	}
	return out
}

// ScheduledPurgeStore is the store seam of the scheduled purge runner.
type ScheduledPurgeStore interface {
	PurgeStore
	ListScheduledComponentPurgePlans(ctx context.Context) ([]store.ComponentPurgePlan, error)
	CancelComponentPurgePlan(ctx context.Context, id string) error
	AdvanceComponentPurgePlans(ctx context.Context, now time.Time) (int, error)
}

// ScheduledPurgeRun is one pass's inputs that only the caller can resolve.
type ScheduledPurgeRun struct {
	Now     time.Time
	Policy  *ScheduledPurgePolicy
	Blocked []string
	// Recheck re-resolves the signed policy inside the purge transaction: it
	// returns nil only when the policy in force still authorises purging the
	// plan's component under the plan's version.
	Recheck store.PurgeRecheck
	// CatalogVersion stamps a new plan.
	CatalogVersion int
}

// RunScheduledPurges applies one pass: advance the time-driven transitions,
// decide, then create, cancel or execute. Every action is audited through
// audit (may be nil). It returns the actions taken and the first error; a
// failed action does not stop the others.
func RunScheduledPurges(ctx context.Context, st ScheduledPurgeStore, run ScheduledPurgeRun, audit func(ctx context.Context, action, detail string)) ([]ScheduledPurgeAction, error) {
	if st == nil {
		return nil, ErrPurgeUnavailable
	}
	now := run.Now
	if now.IsZero() {
		now = time.Now()
	}
	if _, err := st.AdvanceComponentPurgePlans(ctx, now); err != nil {
		return nil, err
	}
	plans, err := st.ListScheduledComponentPurgePlans(ctx)
	if err != nil {
		return nil, err
	}
	actions := PlanScheduledPurges(ScheduledPurgeInput{Now: now, Policy: run.Policy, Blocked: run.Blocked, Plans: plans})
	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, a := range actions {
		var detail string
		switch a.Kind {
		case PurgeActionCreate:
			plan, err := st.CreateComponentPurgePlan(ctx, store.ComponentPurgeSpec{
				Kind: store.PurgeScheduled, Component: a.Component, CatalogVersion: run.CatalogVersion,
				PolicyVersion: a.PolicyVersion, Grace: a.Grace, Now: now,
			})
			if err != nil {
				note(fmt.Errorf("componentsvc: schedule purge of %s: %w", a.Component, err))
				continue
			}
			detail = fmt.Sprintf("component=%s plan=%s grace_until=%s counts=%v", a.Component, plan.ID, plan.GraceUntil.UTC().Format(time.RFC3339), plan.Counts)
		case PurgeActionCancel:
			if err := st.CancelComponentPurgePlan(ctx, a.PlanID); err != nil {
				note(fmt.Errorf("componentsvc: cancel purge plan %s: %w", a.PlanID, err))
				continue
			}
			detail = fmt.Sprintf("component=%s plan=%s", a.Component, a.PlanID)
		case PurgeActionExecute:
			// Audit BEFORE the delete (ledger MODPKG-17 (4)): the intent row
			// exists even when the process dies inside the purge; the outcome
			// row below follows it.
			if audit != nil {
				audit(ctx, "components.purge.scheduled.execute_requested",
					fmt.Sprintf("component=%s plan=%s rule=%s", a.Component, a.PlanID, a.Rule))
			}
			res, err := st.ExecuteComponentPurgePlan(ctx, a.PlanID, run.Recheck, now)
			if err != nil {
				if !errors.Is(err, store.ErrPurgePlanNotExecutable) {
					note(fmt.Errorf("componentsvc: scheduled purge %s: %w", a.PlanID, err))
				}
				detail = fmt.Sprintf("component=%s plan=%s refused: %v", a.Component, a.PlanID, err)
				break
			}
			detail = fmt.Sprintf("component=%s plan=%s deleted=%v skipped=%v kept_sessions=%d", a.Component, a.PlanID, res.Deleted, res.Skipped, res.KeptSessions)
		}
		if audit != nil {
			audit(ctx, "components.purge.scheduled."+string(a.Kind), detail+" rule="+a.Rule)
		}
	}
	return actions, firstErr
}
