package store

import (
	"context"
	"testing"
	"time"
)

// TestLookupProcessControlStopForRun pins T-584: a wrapped dashboard launch's
// PTY child is the `observer <tool>` launcher, while the node-intervention
// audit row names the vendor process the launcher started as its child.
// The exact pid match misses it, so the run's tool and exit time resolve the
// stop instead, inside a tight window around the exit. Each row is one rule
// of the correlation, including the decoys that must never match.
func TestLookupProcessControlStopForRun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	launchedAt := time.Date(2026, 10, 6, 21, 0, 0, 0, time.UTC)
	exitedAt := launchedAt.Add(5 * time.Minute)
	const wrapperPID, vendorPID = 4100, 4101

	type row struct {
		at   time.Time
		pid  int
		tool string
	}
	cases := []struct {
		name      string
		rows      []row
		run       ProcessControlRun
		wantFound bool
		wantRule  string
	}{
		{
			name:      "wrapped launch: vendor child stopped just before the wrapper exited",
			rows:      []row{{exitedAt.Add(-2 * time.Second), vendorPID, "devin"}},
			run:       ProcessControlRun{PID: wrapperPID, LaunchedAt: launchedAt, Tool: "devin", ExitedAt: exitedAt},
			wantFound: true, wantRule: "B-602",
		},
		{
			name:      "audit row written just after the wrapper exited",
			rows:      []row{{exitedAt.Add(3 * time.Second), vendorPID, "devin"}},
			run:       ProcessControlRun{PID: wrapperPID, LaunchedAt: launchedAt, Tool: "devin", ExitedAt: exitedAt},
			wantFound: true, wantRule: "B-602",
		},
		{
			name:      "direct launch still matches by its own pid",
			rows:      []row{{launchedAt.Add(time.Minute), wrapperPID, "devin"}},
			run:       ProcessControlRun{PID: wrapperPID, LaunchedAt: launchedAt},
			wantFound: true, wantRule: "B-602",
		},
		{
			name: "decoy: another tool's stop at the same moment",
			rows: []row{{exitedAt.Add(-time.Second), vendorPID, "claude-code"}},
			run:  ProcessControlRun{PID: wrapperPID, LaunchedAt: launchedAt, Tool: "devin", ExitedAt: exitedAt},
		},
		{
			name: "decoy: same tool stopped long before this run exited",
			rows: []row{{exitedAt.Add(-3 * time.Minute), vendorPID, "devin"}},
			run:  ProcessControlRun{PID: wrapperPID, LaunchedAt: launchedAt, Tool: "devin", ExitedAt: exitedAt},
		},
		{
			name: "decoy: same tool stopped before this run launched",
			rows: []row{{launchedAt.Add(-10 * time.Second), vendorPID, "devin"}},
			run:  ProcessControlRun{PID: wrapperPID, LaunchedAt: launchedAt, Tool: "devin", ExitedAt: launchedAt.Add(5 * time.Second)},
		},
		{
			name: "decoy: the run has not exited, so no tool fallback",
			rows: []row{{launchedAt.Add(time.Minute), vendorPID, "devin"}},
			run:  ProcessControlRun{PID: wrapperPID, LaunchedAt: launchedAt, Tool: "devin"},
		},
		{
			name: "decoy: tool unknown, so no tool fallback",
			rows: []row{{exitedAt.Add(-time.Second), vendorPID, "devin"}},
			run:  ProcessControlRun{PID: wrapperPID, LaunchedAt: launchedAt, ExitedAt: exitedAt},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st, _ := newTestStore(t)
			for _, r := range tc.rows {
				audit := nodeProcessControlAuditFixture(r.at)
				audit.PID = r.pid
				audit.Tool = r.tool
				audit.Status = "terminated"
				audit.RuleID = "B-602"
				audit.Reason = "daily spend accounting is unavailable"
				if err := st.InsertNodeProcessControlAudit(ctx, audit); err != nil {
					t.Fatalf("InsertNodeProcessControlAudit: %v", err)
				}
			}
			stop, found, err := st.LookupProcessControlStopForRun(ctx, tc.run)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if found != tc.wantFound {
				t.Fatalf("found = %v, want %v (stop %+v)", found, tc.wantFound, stop)
			}
			if found && (stop.RuleID != tc.wantRule || stop.Decision != "terminated") {
				t.Errorf("stop = %+v, want rule %s terminated", stop, tc.wantRule)
			}
		})
	}
}
