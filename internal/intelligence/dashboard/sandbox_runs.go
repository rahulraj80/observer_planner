package dashboard

import (
	"net/http"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/runstate"
)

// sandboxRunsScan bounds how many recent terminal runs the Sandboxes page's
// list looks through for sandboxed ones.
const sandboxRunsScan = 200

// sandboxRunRow is one run of GET /api/terminal/sandbox/runs: what the run
// recorded at launch (closed sandboxvocab words, no path, no command) and
// what Observer recorded while it ran. Refused is absent when the run has
// no refusal on record, so the page shows nothing rather than a zero it
// cannot vouch for.
type sandboxRunRow struct {
	RunID      string `json:"run_id"`
	Tool       string `json:"tool"`
	Kind       string `json:"kind"`
	LaunchedAt string `json:"launched_at"`
	EndedAt    string `json:"ended_at,omitempty"`
	// DurationSeconds is ended minus launched, for an ended run only.
	DurationSeconds *int64 `json:"duration_seconds,omitempty"`
	State           string `json:"state"`
	ExitCode        *int   `json:"exit_code,omitempty"`
	EndReason       string `json:"end_reason,omitempty"`
	// Backend is the outermost backend of the run's stack ("bwrap",
	// "sandhold"); Stack is the whole stack, outermost first.
	Backend        string   `json:"backend"`
	Stack          []string `json:"stack,omitempty"`
	IsolationClass string   `json:"isolation_class"`
	EgressTier     string   `json:"egress_tier,omitempty"`
	CaptureLevel   string   `json:"capture_level,omitempty"`
	// Refused is the connections the sandbox refused (the egress refusal
	// report: rows kept, plus those dropped before they were written,
	// evicted to bound the run, or refused while the link was down).
	// RefusedMayBeMore says some refusals could not be told apart from
	// another run's, so the count can be low.
	Refused          *int `json:"refused,omitempty"`
	RefusedMayBeMore bool `json:"refused_may_be_more,omitempty"`
}

// handleSandboxRuns serves GET /api/terminal/sandbox/runs (View): the
// recent sandboxed terminal runs, newest first, for the Terminals >
// Sandboxes tab. A read of the run history and the refusal report's
// counts (never the hosts, which stay behind the Local refusal audit);
// it writes nothing and calls nothing. limit (default 20, at most 100)
// bounds the answer; only the newest sandboxRunsScan runs are looked at.
func (s *Server) handleSandboxRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	st := s.remoteManageStore()
	if st == nil {
		writeJSON(w, map[string]any{"runs": []any{}})
		return
	}
	limit := intArg(r, "limit", 20, 1, 100)
	runs, err := st.ListTerminalRuns(r.Context(), sandboxRunsScan)
	if err != nil {
		writeErr(w, err)
		return
	}
	rules := runstate.DefaultRules()
	now := time.Now().UTC()
	out := make([]sandboxRunRow, 0, limit)
	ids := make([]string, 0, limit)
	for _, rn := range runs {
		if len(out) == limit {
			break
		}
		if rn.IsolationClass == "" || rn.IsolationClass == "none" {
			continue
		}
		row := sandboxRunRow{
			RunID: rn.RunID, Tool: rn.Tool, Kind: rn.Kind,
			LaunchedAt: rn.LaunchedAt.UTC().Format(time.RFC3339),
			ExitCode:   rn.ExitCode, EndReason: rn.EndReason,
			IsolationClass: rn.IsolationClass, EgressTier: rn.EgressTier, CaptureLevel: rn.CaptureLevel,
			State: "ended",
		}
		for _, b := range strings.Split(rn.Stack, ",") {
			if b = strings.TrimSpace(b); b != "" {
				row.Stack = append(row.Stack, b)
			}
		}
		if len(row.Stack) > 0 {
			row.Backend = row.Stack[0]
		}
		if rn.EndedAt != nil {
			row.EndedAt = rn.EndedAt.UTC().Format(time.RFC3339)
			if d := int64(rn.EndedAt.Sub(rn.LaunchedAt).Seconds()); d >= 0 {
				row.DurationSeconds = &d
			}
		} else {
			// The history's own read-time verdict (handleTerminalRuns): a
			// run still open long after launch with no live terminal is
			// abandoned, not running.
			row.State = "running"
			if v := runstate.Reconcile(rules, runstate.Signal{Kind: runstate.KindTerminalRun, Status: "running", Age: now.Sub(rn.LaunchedAt)}); v.Changed {
				row.State = v.To
			}
		}
		out = append(out, row)
		ids = append(ids, rn.RunID)
	}
	counts, err := st.SandboxEgressRefusalCounts(r.Context(), ids)
	if err != nil {
		writeErr(w, err)
		return
	}
	drops, err := st.SandboxEgressRefusalDrops(r.Context(), ids)
	if err != nil {
		writeErr(w, err)
		return
	}
	for i := range out {
		d := drops[out[i].RunID]
		n := counts[out[i].RunID] + d.Dropped + d.Evicted + d.EgressClosed
		if n > 0 {
			out[i].Refused = &n
		}
		out[i].RefusedMayBeMore = d.Uncertain > 0
	}
	writeJSON(w, map[string]any{"runs": out})
}
