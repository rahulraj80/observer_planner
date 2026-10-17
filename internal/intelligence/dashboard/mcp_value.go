package dashboard

import (
	"net/http"
)

// MCP value meter (usability arc P4.10 / review §8.1): turns the E1
// "schema tax" nudge into a standing readout — what the MCP tools
// actually got called vs what their per-turn schema overhead costs.
// Numbers mirror the advisor's mcp_overhead detector (~1,900 schema
// tokens per turn, 3 calls / 100 turns as the worth-it line) so the
// two surfaces never disagree.

// mcpSchemaTokensPerTurn and mcpWorthItCallsPer100 mirror
// advisor.detectMCPOverhead / DefaultThresholds — keep in sync.
const (
	mcpSchemaTokensPerTurn = 1900
	mcpWorthItCallsPer100  = 3.0
)

// handleMCPValue serves GET /api/mcp/value?days=N (default 30), or the
// closed custom range since/until through windowRange (T-550).
func (s *Server) handleMCPValue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	days := intArg(r, "days", 30, 1, 365)
	since, until := windowRange(r, 30, 1, 365)
	cutoff, upper := windowBoundStrings(since, until)
	ctx := r.Context()

	// Audit-off honesty: with [intelligence.mcp.audit] disabled the
	// audit table goes quiet regardless of real usage — "unused" would
	// be a lie, so the verdict short-circuits to no_data.
	auditEnabled := true
	if cfg, err := loadConfigForDashboard(s.opts.ConfigPath); err == nil {
		auditEnabled = cfg.Intelligence.MCP.Audit.Enabled
	}

	var calls, okCalls, bytesReturned int64
	type toolRow struct {
		Tool  string `json:"tool"`
		Calls int64  `json:"calls"`
		Bytes int64  `json:"bytes"`
	}
	byTool := []toolRow{}
	if s.db() != nil {
		// Tolerant reads: a query problem reads as zero rows — the
		// verdict below treats missing data honestly.
		_ = s.db().QueryRowContext(ctx,
			`SELECT COUNT(*), COALESCE(SUM(response_ok), 0), COALESCE(SUM(response_size_bytes), 0)
			 FROM mcp_audit WHERE ts >= ? AND (? = '' OR ts < ?)`, cutoff, upper, upper).Scan(&calls, &okCalls, &bytesReturned)
		rows, err := s.db().QueryContext(ctx,
			`SELECT tool_name, COUNT(*), COALESCE(SUM(response_size_bytes), 0)
			 FROM mcp_audit WHERE ts >= ? AND (? = '' OR ts < ?)
			 GROUP BY tool_name ORDER BY COUNT(*) DESC`, cutoff, upper, upper)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var tr toolRow
				if rows.Scan(&tr.Tool, &tr.Calls, &tr.Bytes) == nil {
					byTool = append(byTool, tr)
				}
			}
			_ = rows.Err()
		}
	}

	// Turn volume estimate: api_turns counts only proxied turns,
	// token_usage only JSONL-visible ones; the advisor's exact
	// proxy∪JSONL dedup is overkill for a meter, so take the larger of
	// the two and label it an estimate.
	var apiTurns, tokenRows int64
	if s.db() != nil {
		_ = s.db().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM api_turns WHERE timestamp >= ? AND (? = '' OR timestamp < ?)`,
			cutoff, upper, upper).Scan(&apiTurns)
		_ = s.db().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM token_usage WHERE timestamp >= ? AND (? = '' OR timestamp < ?)`,
			cutoff, upper, upper).Scan(&tokenRows)
	}
	turns := max(apiTurns, tokenRows)

	callsPer100 := 0.0
	if turns > 0 {
		callsPer100 = float64(calls) * 100 / float64(turns)
	}
	var verdict string
	switch {
	case !auditEnabled:
		verdict = "no_data" // audit off — usage invisible, not absent
	case turns == 0:
		verdict = "no_data"
	case calls == 0:
		verdict = "unused"
	case callsPer100 >= mcpWorthItCallsPer100:
		verdict = "active"
	default:
		verdict = "low_use"
	}

	writeJSON(w, map[string]any{
		"window_days":              days,
		"since":                    windowMetaSince(since, until),
		"until":                    windowMetaUntil(since, until),
		"audit_enabled":            auditEnabled,
		"calls":                    calls,
		"ok_calls":                 okCalls,
		"denied_calls":             calls - okCalls,
		"bytes_returned":           bytesReturned,
		"by_tool":                  byTool,
		"turns_estimate":           turns,
		"calls_per_100_turns":      callsPer100,
		"overhead_tokens_estimate": turns * mcpSchemaTokensPerTurn,
		"schema_tokens_per_turn":   mcpSchemaTokensPerTurn,
		"threshold_calls_per_100":  mcpWorthItCallsPer100,
		"verdict":                  verdict,
	})
}
