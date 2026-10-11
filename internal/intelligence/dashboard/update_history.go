package dashboard

import (
	"net/http"
	"strconv"
	"time"

	"github.com/marmutapp/superbased-observer/internal/store"
)

// updateHistoryEvent is one row of GET /api/update/history: the same fields
// `observer update history` prints (WHEN / FROM / TO / STATE / ERROR /
// DETAIL), read through the same store seam.
type updateHistoryEvent struct {
	ID          int64     `json:"id"`
	At          time.Time `json:"at"`
	FromVersion string    `json:"from_version"`
	ToVersion   string    `json:"to_version"`
	State       string    `json:"state"`
	ErrorClass  string    `json:"error_class,omitempty"`
	Detail      string    `json:"detail"`
	DurationMS  int64     `json:"duration_ms"`
}

// handleUpdateHistory serves GET /api/update/history[?limit=N] (default 50,
// the CLI's default): every apply, rollback and refusal this node recorded,
// newest first (backlog item 6, sweep row 23).
func (s *Server) handleUpdateHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	rows, err := store.New(s.opts.DB).LoadUpdateEvents(r.Context(), limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]updateHistoryEvent, 0, len(rows))
	for _, e := range rows {
		out = append(out, updateHistoryEvent{
			ID: e.ID, At: e.At, FromVersion: e.FromVersion, ToVersion: e.ToVersion,
			State: string(e.State), ErrorClass: string(e.ErrorClass),
			Detail: e.Detail, DurationMS: e.DurationMS,
		})
	}
	writeJSON(w, map[string]any{"events": out})
}
