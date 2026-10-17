package modelfilter

import "strings"

// MaxLen bounds a plausible filter value. Model ids are short identifiers
// ("claude-opus-4-7", "openrouter/qwen/qwen3-coder:free"); anything longer is
// not a value the store holds. See TooLong.
const MaxLen = 200

// Normalize turns a raw `model=` query value into the value the predicates
// bind, or "" for "no filter". Whitespace is trimmed; only absent, empty and
// the UI's unfiltered sentinel "all" mean no filter. An over-long value is
// KEPT (Sol F5): dropping it made a filter the viewer set silently become no
// filter, widening the answer. Kept, it is an exact match that narrows to
// nothing (an unknown model already behaves that way); a caller that would
// rather refuse it checks TooLong.
func Normalize(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" || strings.EqualFold(v, "all") {
		return ""
	}
	return v
}

// TooLong reports whether a normalized value is longer than any model id, so
// a caller can refuse it (the org API answers 400) instead of matching it.
func TooLong(v string) bool { return len(v) > MaxLen }

// sessionsUsingSQL selects the ids of every session that used a model: a
// proxy turn or a transcript token row with that model (D-154). The session's
// own sessions.model is NOT evidence: it is a label (the first or the
// configured model), set even for a session that never ran a turn on it, so
// treating it as use would list sessions with no spend on the model (Sol
// A3-04). It takes the model bound twice.
const sessionsUsingSQL = `SELECT session_id FROM token_usage WHERE model = ? AND session_id IS NOT NULL` +
	` UNION SELECT session_id FROM api_turns WHERE model = ? AND session_id IS NOT NULL`

// SessionsUsing returns the session-grain predicate "sessionCol is a session
// that used model" and its bound args, or ("", nil) when model is "" (no
// filter). sessionCol is a column reference the caller owns (a code
// constant such as "s.id" or "a.session_id"), never user input.
func SessionsUsing(sessionCol, model string) (string, []any) {
	if model == "" {
		return "", nil
	}
	return sessionCol + " IN (" + sessionsUsingSQL + ")", []any{model, model}
}

// ProjectsUsing returns the project-grain predicate "projectCol is a project
// with at least one session that used model", for tables mined per project
// (project_patterns). It is the model sibling of the tool filter's
// "projects with an action from that tool" rule. ("", nil) when model is "".
func ProjectsUsing(projectCol, model string) (string, []any) {
	if model == "" {
		return "", nil
	}
	inner, args := SessionsUsing("id", model)
	return projectCol + " IN (SELECT DISTINCT project_id FROM sessions WHERE " + inner + ")", args
}

// RowIs returns the turn-grain predicate "modelCol equals model" and its
// bound arg, or ("", nil) when model is "" (no filter). modelCol is a column
// reference the caller owns (a code constant), never user input.
func RowIs(modelCol, model string) (string, []any) {
	if model == "" {
		return "", nil
	}
	return modelCol + " = ?", []any{model}
}

// Matches is the in-memory sibling of RowIs for rows a caller already holds in
// Go (a decoded router decision, a benchmark run's model list): it reports
// whether a row whose model columns carry `models` passes the filter. "" (no
// filter) matches everything; otherwise the row matches when ANY of its model
// values equals the filter exactly (a router decision names both the model the
// client asked for and the one it ran on; either is the model "in" that row).
func Matches(filter string, models ...string) bool {
	if filter == "" {
		return true
	}
	for _, m := range models {
		if m == filter {
			return true
		}
	}
	return false
}

// AndSessionsUsing is SessionsUsing as an " AND ..." suffix for callers that
// build WHERE clauses by string concatenation ("" when no filter).
func AndSessionsUsing(sessionCol, model string) (string, []any) {
	frag, args := SessionsUsing(sessionCol, model)
	if frag == "" {
		return "", nil
	}
	return " AND " + frag, args
}

// AndRowIs is RowIs as an " AND ..." suffix ("" when no filter).
func AndRowIs(modelCol, model string) (string, []any) {
	frag, args := RowIs(modelCol, model)
	if frag == "" {
		return "", nil
	}
	return " AND " + frag, args
}
