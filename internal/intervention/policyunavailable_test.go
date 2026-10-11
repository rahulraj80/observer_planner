package intervention

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestPolicyUnavailableAlwaysCarriesRuleAndReason pins the node 1 live
// defect (2026-10-03): a node_process_control policy_unavailable row for
// an agy process had an EMPTY rule_id and reason. Every path that yields
// policy_unavailable must name a rule and a reason.
func TestPolicyUnavailableAlwaysCarriesRuleAndReason(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 3, 5, 1, 0, 0, time.UTC)
	stop := PolicyDecision{Authorized: true, Stop: true, Authority: "grant", ValidUntil: now.Add(time.Minute), RuleID: "B-602", Reason: "daily cap reached"}
	bare := PolicyDecision{Authorized: true, Stop: true, Authority: "grant", ValidUntil: now.Add(time.Minute)}
	w := Workload{SurfaceID: "fixture/native", Identity: Identity{BootID: "boot", PID: 123, StartTicks: 10, UID: 1000, Executable: ExecutableIdentity{Device: 1, Inode: 2}}}
	acquire := func(context.Context, Identity) (ControlHandle, error) { return &supervisorTestHandle{}, nil }
	refuse := func(context.Context, Workload, PolicyDecision, func(context.Context) (ExitResult, error)) (ExitResult, error) {
		return ExitResult{}, context.DeadlineExceeded
	}
	cases := []struct {
		name       string
		s          Supervisor
		wantRule   string
		wantReason string
	}{
		{
			"decide error (binding revalidation)",
			Supervisor{Now: func() time.Time { return now }, Acquire: acquire, Decide: func(context.Context, Workload) (PolicyDecision, error) {
				return PolicyDecision{}, errors.Join(errors.New("x"), ErrInvalidIdentity)
			}},
			PolicyUnavailableRuleID, "the process binding could not be revalidated",
		},
		{
			"decide error (opaque)",
			Supervisor{Now: func() time.Time { return now }, Acquire: acquire, Decide: func(context.Context, Workload) (PolicyDecision, error) {
				return PolicyDecision{}, errors.New("store: database is locked")
			}},
			PolicyUnavailableRuleID, "the policy decision could not be made",
		},
		{
			"no resolver",
			Supervisor{Now: func() time.Time { return now }, Acquire: acquire},
			PolicyUnavailableRuleID, "the policy resolver is unavailable",
		},
		{
			"expired authority",
			Supervisor{Now: func() time.Time { return now }, Acquire: acquire, Decide: func(context.Context, Workload) (PolicyDecision, error) {
				d := stop
				d.ValidUntil = now
				return d, nil
			}},
			PolicyUnavailableRuleID, "the managed authority is missing or expired",
		},
		{
			"fence refused a decided stop",
			Supervisor{Now: func() time.Time { return now }, Acquire: acquire, Fence: refuse, Decide: func(context.Context, Workload) (PolicyDecision, error) { return stop, nil }},
			"B-602", "daily cap reached; not signalled: the policy decision or its signal fence timed out",
		},
		{
			"fence refused a stop that named no rule",
			Supervisor{Now: func() time.Time { return now }, Acquire: acquire, Fence: refuse, Decide: func(context.Context, Workload) (PolicyDecision, error) { return bare, nil }},
			PolicyUnavailableRuleID, "the policy decision or its signal fence timed out",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.s.Reconcile(context.Background(), []Workload{w})
			if len(out) != 1 || out[0].Status != "policy_unavailable" {
				t.Fatalf("outcome = %+v", out)
			}
			if out[0].RuleID != tc.wantRule {
				t.Errorf("rule = %q, want %q", out[0].RuleID, tc.wantRule)
			}
			if out[0].Reason == "" || !strings.Contains(out[0].Reason, tc.wantReason) {
				t.Errorf("reason = %q, want it to contain %q", out[0].Reason, tc.wantReason)
			}
			if strings.Contains(out[0].Reason, "database is locked") {
				t.Errorf("reason leaks the error text: %q", out[0].Reason)
			}
		})
	}
}
