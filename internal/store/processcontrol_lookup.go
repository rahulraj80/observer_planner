package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// processcontrol_lookup.go is the READ seam over the guard_events
// process_control audit rows internal/store/nodeinterventionaudit.go writes
// (InsertNodeProcessControlAudit). It exists so a dashboard terminal surface
// (LaunchTerminal modal + the minimized pill) can explain a terminal exit the
// daemon itself caused — an org-managed node's node-intervention loop
// stopping a vendor process the operator launched from "New Terminal" — with
// the SAME audit row the daemon already wrote, rather than guessing at a bare
// exit code. This file only reads; InsertNodeProcessControlAudit remains the
// one writer (CLAUDE.md module-boundary rule #4).

// ProcessControlStop is one guard_events process_control audit row, decoded
// down to what a UI surface may show: the rule that fired, the outcome
// decision, the bounded human-readable reason, and when it happened. It
// deliberately excludes the audit row's process-identity fields
// (pid/uid/boot_id/executable device+inode) and policy/pricing fingerprints —
// those are attribution/forensics metadata, not something to echo to an
// operator explaining why their terminal closed.
type ProcessControlStop struct {
	// RuleID is the policy rule that fired (e.g. "B-602").
	RuleID string
	// Decision is the outcome status nodeProcessControlStatus normalized
	// (e.g. "terminated", "killed", "policy_unavailable").
	Decision string
	// Reason is the bounded human-readable explanation the intervention
	// outcome carried (nodeProcessControlReasonMetadata.Reason).
	Reason string
	// At is the audit row's timestamp (guard_events.ts).
	At time.Time
}

// processControlStopLookupLimit bounds the guard_events scan to the newest N
// process_control rows within the window: a terminal run is stopped by the
// node-intervention loop at most once, so the newest match in the window is
// authoritative, and this keeps the read a single bounded query rather than an
// unbounded table scan on a busy managed node.
const processControlStopLookupLimit = 50

// nodeProcessControlReasonView is the subset of the writer-side
// nodeProcessControlReasonMetadata (nodeinterventionaudit.go) this read seam
// needs to decode from guard_events.reason. Declared separately from the
// writer's type — even though both live in this package — so this seam only
// ever depends on the two JSON keys it actually reads and stays correct even
// if the writer's metadata struct grows unrelated fields.
type nodeProcessControlReasonView struct {
	PID    int    `json:"pid"`
	Tool   string `json:"tool"`
	Reason string `json:"reason"`
}

// ProcessControlRun is one terminal run as LookupProcessControlStopForRun
// correlates it: the PTY child's pid and launch time, plus, once the run has
// exited, its canonical tool and exit time.
type ProcessControlRun struct {
	// PID is the PTY child's OS pid. For a wrapped launch that is the
	// `observer <tool>` launcher, not the vendor process it starts.
	PID int
	// LaunchedAt bounds every match from below.
	LaunchedAt time.Time
	// Tool is the run's canonical tool (sessions.tool vocabulary); "" when
	// unknown, which disables the tool-window fallback.
	Tool string
	// ExitedAt is when the run's child exited; zero while it runs, which
	// disables the tool-window fallback.
	ExitedAt time.Time
}

// processControlStopToolWindow is how far either side of a run's exit an
// audit row for the run's tool may sit to explain that exit. A wrapped
// launch exits within moments of the node-intervention loop stopping its
// vendor child, and the audit row is written once the stop is observed, so
// the two times are seconds apart; the window absorbs a slow observation
// without reaching an unrelated stop minutes away.
const processControlStopToolWindow = 30 * time.Second

// LookupProcessControlStopForRun resolves the policy stop that explains one
// terminal run's exit (T-584). It first matches the run's own pid
// (LookupProcessControlStop, exact). A wrapped launch's PTY child is the
// `observer <tool>` launcher, which starts the vendor process as a CHILD, so
// the audit row names that child's pid and the exact match misses; the
// fallback then matches an audit row for the run's tool whose time lies
// within processControlStopToolWindow of the run's exit (and not before its
// launch). The fallback applies only once the run has exited and its tool is
// known; otherwise found is false, never a guess.
func (s *Store) LookupProcessControlStopForRun(ctx context.Context, run ProcessControlRun) (stop ProcessControlStop, found bool, err error) {
	stop, found, err = s.LookupProcessControlStop(ctx, run.PID, run.LaunchedAt)
	if err != nil || found {
		return stop, found, err
	}
	if run.Tool == "" || run.ExitedAt.IsZero() {
		return ProcessControlStop{}, false, nil
	}
	from := run.ExitedAt.Add(-processControlStopToolWindow)
	if run.LaunchedAt.After(from) {
		from = run.LaunchedAt
	}
	to := run.ExitedAt.Add(processControlStopToolWindow)
	// The SQL bound is a lexical compare over RFC3339Nano text, whose
	// trimmed fractional seconds can misorder values within one second;
	// widen it by a second and let the parsed-time predicate decide.
	return s.scanProcessControlStops(ctx, "store.LookupProcessControlStopForRun", from.Add(-time.Second), func(view nodeProcessControlReasonView, at time.Time) bool {
		return view.Tool == run.Tool && !at.Before(from) && !at.After(to)
	})
}

// LookupProcessControlStop returns the newest guard_events process_control
// audit row (category=process_control, event_kind=NodeProcessControlEventKind)
// whose reason metadata names pid, among rows with ts >= since. found is false
// when no such row exists in the window — the honest "no policy stop on
// record", never a guess. since is normally the terminal run's launch time, so
// a stale audit row from an earlier, unrelated process that happened to reuse
// the same OS pid is never matched.
func (s *Store) LookupProcessControlStop(ctx context.Context, pid int, since time.Time) (stop ProcessControlStop, found bool, err error) {
	if s == nil || s.db == nil {
		return ProcessControlStop{}, false, errors.New("store.LookupProcessControlStop: store unavailable")
	}
	if ctx == nil {
		return ProcessControlStop{}, false, errors.New("store.LookupProcessControlStop: nil context")
	}
	if pid <= 0 {
		return ProcessControlStop{}, false, nil
	}
	return s.scanProcessControlStops(ctx, "store.LookupProcessControlStop", since, func(view nodeProcessControlReasonView, _ time.Time) bool {
		return view.PID == pid
	})
}

// scanProcessControlStops walks the newest process_control audit rows with
// ts >= since (bounded by processControlStopLookupLimit) and returns the
// first whose decoded reason metadata and parsed time satisfy match. op
// names the caller in wrapped errors.
func (s *Store) scanProcessControlStops(ctx context.Context, op string, since time.Time, match func(view nodeProcessControlReasonView, at time.Time) bool) (ProcessControlStop, bool, error) {
	if s == nil || s.db == nil {
		return ProcessControlStop{}, false, errors.New(op + ": store unavailable")
	}
	if ctx == nil {
		return ProcessControlStop{}, false, errors.New(op + ": nil context")
	}
	rows, qerr := s.db.QueryContext(ctx, `
		SELECT ts, rule_id, decision, reason
		  FROM guard_events
		 WHERE category = ? AND event_kind = ? AND ts >= ?
		 ORDER BY ts DESC, id DESC
		 LIMIT ?`,
		nodeProcessControlCategory, NodeProcessControlEventKind, timestamp(since), processControlStopLookupLimit)
	if qerr != nil {
		return ProcessControlStop{}, false, fmt.Errorf("%s: query: %w", op, qerr)
	}
	defer rows.Close()

	for rows.Next() {
		var tsStr, ruleID, decision string
		var reason sql.NullString
		if serr := rows.Scan(&tsStr, &ruleID, &decision, &reason); serr != nil {
			return ProcessControlStop{}, false, fmt.Errorf("%s: scan: %w", op, serr)
		}
		if !reason.Valid || reason.String == "" {
			continue
		}
		var view nodeProcessControlReasonView
		if jerr := json.Unmarshal([]byte(reason.String), &view); jerr != nil {
			continue // a malformed/foreign reason payload is skipped, never fatal
		}
		at, perr := time.Parse(time.RFC3339Nano, tsStr)
		if perr != nil {
			at = time.Time{}
		}
		if !match(view, at) {
			continue
		}
		return ProcessControlStop{
			RuleID:   ruleID,
			Decision: decision,
			Reason:   view.Reason,
			At:       at,
		}, true, nil
	}
	if rerr := rows.Err(); rerr != nil {
		return ProcessControlStop{}, false, fmt.Errorf("%s: rows: %w", op, rerr)
	}
	return ProcessControlStop{}, false, nil
}
