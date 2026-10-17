package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/marmutapp/superbased-observer/internal/budgetwarnfile"
)

// budgetstatus_stream.go is budget freshness P4 (T-206, D-016): the node's
// org budget state pushed to the dashboard as server-sent events, so a
// crossed threshold reaches an open dashboard within seconds of the daemon
// writing budget-status.json instead of on the banner's next 15 s poll.
//
// The stream carries exactly the GET /api/budget/status payload (same
// route class, same never-hidden section: a warning about the developer's
// own spend), sent once on connect and again whenever it changes. The
// banner keeps its 15 s poll as the fallback for a browser or proxy that
// cannot hold the stream open.

// Stream timing. Package variables so tests run in milliseconds.
var (
	// budgetStreamPoll is how often the handler re-reads the status file.
	budgetStreamPoll = 2 * time.Second
	// budgetStreamHeartbeat keeps intermediaries from closing an idle stream.
	budgetStreamHeartbeat = 25 * time.Second
	// budgetStreamMaxLife ends a stream so the browser reconnects (and
	// re-authenticates) at a bounded interval.
	budgetStreamMaxLife = 10 * time.Minute
	// budgetStreamRequestableEvery bounds how often one stream re-reads the
	// requestable budgets (REVIEW-4 P4-14). The status file itself is still
	// read every poll: it is the fast signal the stream exists for.
	budgetStreamRequestableEvery = 30 * time.Second
	// budgetStreamMax is the process-wide cap on concurrent streams
	// (REVIEW-4 P4-14). The route is reachable from the viewer listener, and
	// each stream is a 10-minute resource; over the cap the request gets 503
	// and the banner keeps its 15 s poll.
	budgetStreamMax int64 = 16
)

// budgetStreamActive counts the open streams (see budgetStreamMax).
var budgetStreamActive atomic.Int64

// budgetStreamRetryMS is the reconnect delay the stream asks the browser for.
const budgetStreamRetryMS = 5000

// handleBudgetStatusStream serves GET /api/budget/status/stream.
func (s *Server) handleBudgetStatusStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	if budgetStreamActive.Add(1) > budgetStreamMax {
		budgetStreamActive.Add(-1)
		w.Header().Set("Retry-After", "30")
		http.Error(w, "too many budget status streams; the banner falls back to polling", http.StatusServiceUnavailable)
		return
	}
	defer budgetStreamActive.Add(-1)
	cfg, err := loadConfigForDashboard(s.opts.ConfigPath)
	if err != nil {
		writeErr(w, fmt.Errorf("load config: %w", err))
		return
	}
	path := budgetwarnfile.StatusPath(cfg.Observer.DBPath)
	// The server's write timeout would cut a long stream; this one ends
	// itself after budgetStreamMaxLife instead.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, "retry: %d\n\n", budgetStreamRetryMS); err != nil {
		return
	}
	flusher.Flush()

	last := ""
	requestable := s.requestableBudgets()
	requestableAt := time.Now()
	send := func() bool {
		if time.Since(requestableAt) >= budgetStreamRequestableEvery {
			requestable, requestableAt = s.requestableBudgets(), time.Now()
		}
		body, err := json.Marshal(budgetStatusPayload(path, requestable))
		if err != nil || string(body) == last {
			return err == nil
		}
		last = string(body)
		if _, err := fmt.Fprintf(w, "event: status\ndata: %s\n\n", body); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !send() {
		return
	}
	poll := time.NewTicker(budgetStreamPoll)
	defer poll.Stop()
	beat := time.NewTicker(budgetStreamHeartbeat)
	defer beat.Stop()
	end := time.NewTimer(budgetStreamMaxLife)
	defer end.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-end.C:
			return
		case <-poll.C:
			if !send() {
				return
			}
		case <-beat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
