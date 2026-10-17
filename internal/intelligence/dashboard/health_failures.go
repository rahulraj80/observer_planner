package dashboard

import (
	"net/http"
	"time"
)

// Failures surface (usability arc P4.11 / review §8.1): the
// failure_context table has carried recovered-vs-not per failed
// command since v1, with zero UI. One endpoint, grouped the way the
// operator thinks — "what command keeps failing, did it ever pass,
// where do I look" — rendered as a card on the Health section.

// failureGroup is one command's failure history in the window.
type failureGroup struct {
	Command       string `json:"command"`
	Fails         int64  `json:"fails"`
	Retries       int64  `json:"retries"`
	Recovered     bool   `json:"recovered"`
	LastAt        string `json:"last_at"`
	ErrorCategory string `json:"error_category,omitempty"`
	ErrorMessage  string `json:"error_message,omitempty"`
	SessionID     string `json:"session_id"`
	Project       string `json:"project,omitempty"`
}

// handleHealthFailures serves GET /api/health/failures?days=N
// (default 7, max 30), or the closed custom range since/until through
// windowRange like every other windowed handler (T-550). Groups by command_hash; the bare columns ride
// SQLite's documented MAX()-row semantics, so error/session/project
// come from each group's most recent failure.
func (s *Server) handleHealthFailures(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	days := intArg(r, "days", 7, 1, 30)
	since, until := windowRange(r, 7, 1, 30)
	cutoff, upper := windowBoundStrings(since, until)

	groups := []failureGroup{}
	var total int64
	if s.db() != nil {
		_ = s.db().QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM failure_context
			 WHERE timestamp >= ? AND (? = '' OR timestamp < ?)`, cutoff, upper, upper).Scan(&total)
		rows, err := s.db().QueryContext(r.Context(),
			`SELECT f.command_summary, COUNT(*), COALESCE(SUM(f.retry_count), 0),
			        MAX(f.eventually_succeeded), MAX(f.timestamp),
			        COALESCE(f.error_category, ''), COALESCE(f.error_message, ''),
			        f.session_id, COALESCE(p.name, '')
			 FROM failure_context f
			 LEFT JOIN projects p ON p.id = f.project_id
			 WHERE f.timestamp >= ? AND (? = '' OR f.timestamp < ?)
			 GROUP BY f.command_hash
			 ORDER BY MAX(f.timestamp) DESC
			 LIMIT 50`, cutoff, upper, upper)
		if err != nil {
			writeErr(w, err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var g failureGroup
			var recovered int64
			if rows.Scan(&g.Command, &g.Fails, &g.Retries, &recovered, &g.LastAt,
				&g.ErrorCategory, &g.ErrorMessage, &g.SessionID, &g.Project) == nil {
				g.Recovered = recovered > 0
				groups = append(groups, g)
			}
		}
		_ = rows.Err()
	}

	writeJSON(w, map[string]any{
		"window_days": days,
		"since":       windowMetaSince(since, until),
		"until":       windowMetaUntil(since, until),
		"total":       total,
		"failures":    groups,
	})
}

// windowBoundStrings renders a windowRange result as the two string
// bounds a `col >= ? AND (? = ” OR col < ?)` clause binds. A zero since
// matches every row; a zero until is the open "until now" window.
func windowBoundStrings(since, until time.Time) (lower, upper string) {
	if !since.IsZero() {
		lower = since.UTC().Format(time.RFC3339Nano)
	}
	if !until.IsZero() {
		upper = until.UTC().Format(time.RFC3339Nano)
	}
	return lower, upper
}

// windowMetaSince and windowMetaUntil echo the window actually served
// (windowMeta's RFC3339 rendering) as additive response fields.
func windowMetaSince(since, until time.Time) string {
	s, _ := windowMeta(since, until)
	return s
}

func windowMetaUntil(since, until time.Time) string {
	_, u := windowMeta(since, until)
	return u
}
