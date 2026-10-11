package componentsvc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestPlanScheduledPurgesRows is one case per row of scheduledPurgeRows
// (T-682), plus the component that has no reviewed purge plan.
func TestPlanScheduledPurgesRows(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	pol := &ScheduledPurgePolicy{Version: "v1", AfterDays: 30}
	purgeable := func(id string) bool { return id != "nodata" }
	plan := func(id, version string, state store.PurgeState, grace *time.Time) store.ComponentPurgePlan {
		return store.ComponentPurgePlan{ID: id, Kind: store.PurgeScheduled, Component: "adapter.x", PolicyVersion: version, State: state, GraceUntil: grace}
	}
	for _, tc := range []struct {
		name    string
		policy  *ScheduledPurgePolicy
		blocked []string
		plans   []store.ComponentPurgePlan
		want    []ScheduledPurgeAction
	}{
		{
			// MODPKG-17 (3): a policy in force only as last known good holds a
			// plan past its grace (no execute) and a withdrawn-looking one (no
			// cancel), and schedules nothing new.
			name: "policy held as last known good: hold everything", policy: &ScheduledPurgePolicy{Version: "v1", AfterDays: 30, Held: true},
			blocked: []string{"adapter.x", "adapter.y"},
			plans:   []store.ComponentPurgePlan{plan("p1", "v1", store.PurgeEligible, &past)},
		},
		{
			name: "open plan no longer authorised: cancel (policy withdrawn)", policy: nil, blocked: []string{"adapter.x"},
			plans: []store.ComponentPurgePlan{plan("p1", "v1", store.PurgeGracePending, &future)},
			want:  []ScheduledPurgeAction{{Kind: PurgeActionCancel, Component: "adapter.x", PlanID: "p1"}},
		},
		{
			name: "open plan no longer authorised: cancel (new version)", policy: &ScheduledPurgePolicy{Version: "v2", AfterDays: 30}, blocked: []string{"adapter.x"},
			plans: []store.ComponentPurgePlan{plan("p1", "v1", store.PurgeGracePending, &future)},
			want:  []ScheduledPurgeAction{{Kind: PurgeActionCancel, Component: "adapter.x", PlanID: "p1"}},
		},
		{
			name: "open plan no longer authorised: cancel (component active again)", policy: pol, blocked: nil,
			plans: []store.ComponentPurgePlan{plan("p1", "v1", store.PurgeEligible, &past)},
			want:  []ScheduledPurgeAction{{Kind: PurgeActionCancel, Component: "adapter.x", PlanID: "p1"}},
		},
		{
			name: "open plan past its grace: execute", policy: pol, blocked: []string{"adapter.x"},
			plans: []store.ComponentPurgePlan{plan("p1", "v1", store.PurgeGracePending, &past)},
			want:  []ScheduledPurgeAction{{Kind: PurgeActionExecute, Component: "adapter.x", PlanID: "p1"}},
		},
		{
			name: "open plan in grace: wait", policy: pol, blocked: []string{"adapter.x"},
			plans: []store.ComponentPurgePlan{plan("p1", "v1", store.PurgeGracePending, &future)},
		},
		{
			name: "not authorised: nothing (data kept)", policy: nil, blocked: []string{"adapter.x"},
		},
		{
			name: "already purged under this policy: nothing", policy: pol, blocked: []string{"adapter.x"},
			plans: []store.ComponentPurgePlan{plan("p0", "v1", store.PurgeConsumed, &past)},
		},
		{
			name: "expired unexecuted: reschedule with the grace only", policy: pol, blocked: []string{"adapter.x"},
			plans: []store.ComponentPurgePlan{plan("p0", "v1", store.PurgeExpired, &past)},
			want:  []ScheduledPurgeAction{{Kind: PurgeActionCreate, Component: "adapter.x", Grace: store.ScheduledPurgeGrace, PolicyVersion: "v1"}},
		},
		{
			name: "newly blocked: schedule after the delay plus the grace", policy: pol, blocked: []string{"adapter.x", "nodata"},
			want: []ScheduledPurgeAction{{Kind: PurgeActionCreate, Component: "adapter.x", Grace: 30*24*time.Hour + store.ScheduledPurgeGrace, PolicyVersion: "v1"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := PlanScheduledPurges(ScheduledPurgeInput{Now: now, Policy: tc.policy, Blocked: tc.blocked, Purgeable: purgeable, Plans: tc.plans})
			if len(got) != len(tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
			for i := range got {
				g, w := got[i], tc.want[i]
				if g.Kind != w.Kind || g.Component != w.Component || g.PlanID != w.PlanID || g.Grace != w.Grace || g.PolicyVersion != w.PolicyVersion || g.Rule == "" {
					t.Fatalf("action %d = %+v, want %+v", i, g, w)
				}
			}
		})
	}
}

// fakePurgeStore records the runner's calls.
type fakePurgeStore struct {
	plans     []store.ComponentPurgePlan
	created   []store.ComponentPurgeSpec
	cancelled []string
	executed  []string
	execErr   error
}

func (f *fakePurgeStore) CreateComponentPurgePlan(_ context.Context, spec store.ComponentPurgeSpec) (store.ComponentPurgePlan, error) {
	f.created = append(f.created, spec)
	g := spec.Now.Add(spec.Grace)
	return store.ComponentPurgePlan{ID: "new", Component: spec.Component, GraceUntil: &g}, nil
}

func (f *fakePurgeStore) ExecuteComponentPurgePlan(_ context.Context, id string, recheck store.PurgeRecheck, _ time.Time) (store.PurgeResult, error) {
	f.executed = append(f.executed, id)
	if recheck == nil {
		return store.PurgeResult{}, errors.New("no recheck")
	}
	return store.PurgeResult{}, f.execErr
}

func (f *fakePurgeStore) ComponentPurgePlanByID(context.Context, string) (store.ComponentPurgePlan, error) {
	return store.ComponentPurgePlan{}, store.ErrPurgePlanNotFound
}

func (f *fakePurgeStore) ListScheduledComponentPurgePlans(context.Context) ([]store.ComponentPurgePlan, error) {
	return f.plans, nil
}

func (f *fakePurgeStore) CancelComponentPurgePlan(_ context.Context, id string) error {
	f.cancelled = append(f.cancelled, id)
	return nil
}

func (f *fakePurgeStore) AdvanceComponentPurgePlans(context.Context, time.Time) (int, error) {
	return 0, nil
}

// TestRunScheduledPurgesAppliesAndAudits drives the runner end to end over a
// fake store: create, execute (with the recheck passed through) and cancel,
// each audited; a refused execution is audited, not an error.
func TestRunScheduledPurgesAppliesAndAudits(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	st := &fakePurgeStore{plans: []store.ComponentPurgePlan{
		{ID: "due", Kind: store.PurgeScheduled, Component: "adapter.codex", PolicyVersion: "v1", State: store.PurgeGracePending, GraceUntil: &past},
		{ID: "stale", Kind: store.PurgeScheduled, Component: "adapter.cline", PolicyVersion: "v0", State: store.PurgeGracePending, GraceUntil: &future},
	}, execErr: store.ErrPurgePlanNotExecutable}
	var audits []string
	actions, err := RunScheduledPurges(context.Background(), st, ScheduledPurgeRun{
		Now: now, Policy: &ScheduledPurgePolicy{Version: "v1", AfterDays: 7},
		Blocked: []string{"adapter.codex", "adapter.cline", "adapter.claude-code"},
		Recheck: func(context.Context, store.ComponentPurgePlan) error { return nil },
	}, func(_ context.Context, action, detail string) { audits = append(audits, action) })
	if err != nil {
		t.Fatal(err)
	}
	// codex executes, cline's plan from another version is cancelled (a new
	// one is created on the next pass), claude-code is newly scheduled.
	if len(actions) != 3 || len(st.created) != 1 || len(st.executed) != 1 || len(st.cancelled) != 1 {
		t.Fatalf("actions=%+v created=%+v executed=%v cancelled=%v", actions, st.created, st.executed, st.cancelled)
	}
	if st.created[0].Kind != store.PurgeScheduled || st.created[0].PolicyVersion != "v1" {
		t.Fatalf("created = %+v", st.created[0])
	}
	// Four rows: the execution writes its request BEFORE the delete
	// (MODPKG-17 (4)) and its outcome after.
	if len(audits) != 4 {
		t.Fatalf("audits = %v", audits)
	}
	for i, a := range audits {
		if a == "components.purge.scheduled.execute" && (i == 0 || audits[i-1] != "components.purge.scheduled.execute_requested") {
			t.Fatalf("the execution outcome was not preceded by its request: %v", audits)
		}
	}
	if _, err := RunScheduledPurges(context.Background(), nil, ScheduledPurgeRun{}, nil); !errors.Is(err, ErrPurgeUnavailable) {
		t.Fatalf("nil store: %v", err)
	}
}
