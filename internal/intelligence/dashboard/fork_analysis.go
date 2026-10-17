package dashboard

import (
	"context"
	"strings"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
)

// observer_planner fork helpers for the Analysis latency block and the Daily
// Briefing, kept out of upstream files so upstream merges stay small.

// analysisScopeClause returns the JOIN+WHERE additions and args that
// implement the global Tool/Project filters for a handler scanning
// api_turns or token_usage. alias is the row alias used in the SELECT ("at"
// for api_turns, "tu" for token_usage); table is the SQL table name. The
// returned joins string is appended after FROM <table> <alias>; whereExtra
// is AND-joined onto the WHERE clause; args are appended in placeholder
// order matching whereExtra.
//
// For api_turns the project column lives on api_turns.project_id; for
// token_usage it is reached through sessions.project_id. Tool always comes
// from sessions.tool.
func analysisScopeClause(table, alias, tool, project string) (joins string, whereExtra string, args []any) {
	if tool == "" && project == "" {
		return "", "", nil
	}
	var jb, wb []string
	if table == "api_turns" {
		if project != "" {
			jb = append(jb, "LEFT JOIN projects p ON p.id = "+alias+".project_id")
			wb = append(wb, "p.root_path = ?")
			args = append(args, project)
		}
		if tool != "" {
			jb = append(jb, "LEFT JOIN sessions s ON s.id = "+alias+".session_id")
			wb = append(wb, "s.tool = ?")
			args = append(args, tool)
		}
	} else { // token_usage
		jb = append(jb, "LEFT JOIN sessions s ON s.id = "+alias+".session_id")
		if project != "" {
			jb = append(jb, "LEFT JOIN projects p ON p.id = s.project_id")
			wb = append(wb, "p.root_path = ?")
			args = append(args, project)
		}
		if tool != "" {
			wb = append(wb, "s.tool = ?")
			args = append(args, tool)
		}
	}
	joins = " " + strings.Join(jb, " ")
	if len(wb) > 0 {
		whereExtra = " AND " + strings.Join(wb, " AND ")
	}
	return joins, whereExtra, args
}

// periodProxyTurnStats returns the proxy-path (api_turns) latency samples and
// turn/error counts for [start, end], honoring the Tool/Project scope.
func (s *Server) periodProxyTurnStats(ctx context.Context, start, end time.Time, tool, project string) (ttft, total []int64, turns, errs int64, err error) {
	joins, where, args := analysisScopeClause("api_turns", "at", tool, project)
	q := `SELECT COALESCE(at.time_to_first_token_ms, 0), COALESCE(at.total_response_ms, 0),
	             CASE WHEN COALESCE(at.error_class, '') != '' THEN 1 ELSE 0 END
	        FROM api_turns at` + joins + `
	       WHERE at.timestamp >= ? AND at.timestamp <= ?` + where //nolint:gosec // G202: fragments are code constants; values bind via args.
	all := append([]any{start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano)}, args...)
	rows, err := s.db().QueryContext(ctx, q, all...)
	if err != nil {
		return nil, nil, 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var t, tot int64
		var isErr int
		if err := rows.Scan(&t, &tot, &isErr); err != nil {
			return nil, nil, 0, 0, err
		}
		turns++
		if isErr == 1 {
			errs++
		}
		if t > 0 {
			ttft = append(ttft, t)
		}
		if tot > 0 {
			total = append(total, tot)
		}
	}
	return ttft, total, turns, errs, rows.Err()
}

// writeConfigToml persists cfg to path. Callers hold configWriteMu across
// their load -> patch -> validate -> write span (cta.go, llm_settings.go).
func writeConfigToml(path string, cfg config.Config) error {
	return config.WriteToml(path, cfg)
}
