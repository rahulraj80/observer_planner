package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// "Ask your past" retrieval seam (Phase D Stage 1 item 4): BM25-ranked
// retrieval over the user's own persisted, scrubbed session summaries
// (sessions.summary_md, FTS5 index migration 084), used to ground the
// ask-your-past LLM surface. Node-local only: the FTS table is never selected
// by SelectUnpushedSince, and the prompt builder (llmrun) additionally
// reduces project roots to a bare <repo> marker before any opt-in egress.

const (
	// defaultPastLimit and maxPastLimit bound the retrieval result set.
	defaultPastLimit = 6
	maxPastLimit     = 12
)

// PastQuery selects sessions whose summaries are most relevant to a question.
type PastQuery struct {
	// Since/Until bound sessions by started_at (zero = unbounded).
	Since, Until time.Time
	// Terms are the pre-tokenized content terms (see PastSearchTerms).
	Terms []string
	// Project filters to one project root.
	Project string
	// Limit caps returned sessions (0 → defaultPastLimit, max maxPastLimit).
	Limit int
}

// PastSession is one retrieval hit: the user's own persisted summary plus the
// failure context the ask-your-past prompt grounds on.
type PastSession struct {
	SessionID     string
	Tool          string
	Project       string
	Model         string
	GitBranch     string
	StartedAt     time.Time
	SummaryMD     string
	ActionCount   int
	FailureCount  int
	ErrorMessages []string // top recurring error messages from api_turns
	Score         float64  // BM25 relevance (more negative = less relevant)
}

// stopwords are dropped from a question before FTS matching. Small and
// English-oriented; the remaining content terms still drive the search.
var stopwords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "of": true,
	"to": true, "in": true, "on": true, "at": true, "for": true, "with": true,
	"did": true, "do": true, "does": true, "what": true, "when": true,
	"where": true, "which": true, "who": true, "why": true, "how": true,
	"i": true, "we": true, "you": true, "my": true, "me": true, "is": true,
	"was": true, "were": true, "have": true, "has": true, "had": true,
	"been": true, "about": true, "from": true, "into": true, "last": true,
	"this": true, "that": true, "these": true, "those": true, "day": true,
	"session": true, "sessions": true, "work": true, "worked": true,
	"during": true, "before": true, "after": true,
}

// PastSearchTerms tokenizes a question into the content terms used for FTS5
// matching: lowercased alphanumeric tokens, stopwords dropped, terms shorter
// than two characters dropped.
func PastSearchTerms(question string) []string {
	fields := strings.FieldsFunc(question, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var out []string
	for _, f := range fields {
		f = strings.ToLower(f)
		if len(f) < 2 || stopwords[f] {
			continue
		}
		out = append(out, f)
	}
	return out
}

// sanitizePastTerm reduces an arbitrary term to its alphanumeric characters,
// the safe form used in both the FTS5 MATCH expression and the occurrence
// count expression (immune to FTS operator/quote injection).
func sanitizePastTerm(term string) string {
	var b strings.Builder
	for _, r := range term {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ftsMatchExpr renders the FTS5 MATCH expression for the terms: each term is
// reduced to its alphanumeric characters (immune to FTS operator/quote
// injection — PastSearchTerms already yields such tokens, but the seam accepts
// arbitrary input), double-quoted as a literal phrase, and joined with AND, so
// a session must mention every content term to match.
func ftsMatchExpr(terms []string) string {
	var quoted []string
	for _, t := range terms {
		if s := sanitizePastTerm(t); s != "" {
			quoted = append(quoted, `"`+s+`"`)
		}
	}
	return strings.Join(quoted, " AND ")
}

// pastOccurrenceExpr builds a SQL expression counting how many times the query
// terms occur in a session's summary, and returns the bound args for its `?`
// placeholders (one lowercased term per placeholder, two per term: REPLACE
// needle and LENGTH divisor). The placeholders sit in the SELECT list, so the
// returned args must be bound before any WHERE-clause args. Counts are
// character-accurate for non-ASCII text because SQLite LENGTH counts
// characters and REPLACE operates on them.
func pastOccurrenceExpr(sanitized []string) (string, []any) {
	if len(sanitized) == 0 {
		return "0", nil
	}
	var parts []string
	var args []any
	for _, t := range sanitized {
		parts = append(parts, `(LENGTH(LOWER(s.summary_md)) - LENGTH(REPLACE(LOWER(s.summary_md), ?, ''))) / LENGTH(?)`)
		args = append(args, t, t)
	}
	return "(" + strings.Join(parts, " + ") + ")", args
}

// SelectPastSessions ranks sessions by relevance of their summaries to the
// query terms, within the window and project filter, and returns up to Limit
// hits with per-session failure context (top recurring error messages) for the
// ask-your-past prompt. Ranking is by raw term-occurrence count first (a
// session that keeps mentioning a topic outranks one passing mention — BM25
// alone degenerates to noise on small personal corpora), then BM25, then
// recency. An empty Terms slice yields no hits.
func SelectPastSessions(ctx context.Context, db *sql.DB, q PastQuery) ([]PastSession, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = defaultPastLimit
	}
	if limit > maxPastLimit {
		limit = maxPastLimit
	}
	if len(q.Terms) == 0 {
		return nil, nil
	}

	var sanitized []string
	for _, t := range q.Terms {
		if s := sanitizePastTerm(t); s != "" {
			sanitized = append(sanitized, strings.ToLower(s))
		}
	}
	if len(sanitized) == 0 {
		return nil, nil
	}

	clauses := []string{"f.summary_md MATCH ?"}
	args := []any{ftsMatchExpr(q.Terms)}
	if !q.Since.IsZero() {
		clauses = append(clauses, "s.started_at >= ?")
		args = append(args, q.Since.UTC().Format(time.RFC3339Nano))
	}
	if !q.Until.IsZero() {
		clauses = append(clauses, "s.started_at < ?")
		args = append(args, q.Until.UTC().Format(time.RFC3339Nano))
	}
	if q.Project != "" {
		clauses = append(clauses, "p.root_path = ?")
		args = append(args, q.Project)
	}
	// The occurrence expression's placeholders appear in the SELECT list,
	// textually before the WHERE-clause placeholders, so its args go first.
	occurrenceExpr, occurrenceArgs := pastOccurrenceExpr(sanitized)
	args = append(occurrenceArgs, args...)
	//nolint:gosec // G202: SQL structure is code-constant; occurrenceExpr and clauses are parameter-bound fragments; every value is a ? arg.
	query := `
SELECT s.id, s.tool, COALESCE(p.root_path, ''), COALESCE(s.model, ''),
       COALESCE(s.git_branch, ''), s.started_at, s.summary_md,
       (SELECT COUNT(*) FROM actions a WHERE a.session_id = s.id) AS action_count,
       (SELECT COUNT(*) FROM actions a WHERE a.session_id = s.id AND a.success = 0) AS failure_count,
       bm25(session_summaries_fts) AS score, ` + occurrenceExpr + ` AS occurrences
FROM session_summaries_fts f
JOIN sessions s ON s.rowid = f.rowid
LEFT JOIN projects p ON p.id = s.project_id
WHERE ` + strings.Join(clauses, " AND ") + `
ORDER BY occurrences DESC, bm25(session_summaries_fts) DESC, s.started_at DESC
LIMIT ?`
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store.SelectPastSessions: %w", err)
	}
	defer rows.Close()

	var out []PastSession
	for rows.Next() {
		var ps PastSession
		var started string
		var occurrences int
		if err := rows.Scan(&ps.SessionID, &ps.Tool, &ps.Project, &ps.Model,
			&ps.GitBranch, &started, &ps.SummaryMD,
			&ps.ActionCount, &ps.FailureCount, &ps.Score, &occurrences); err != nil {
			return nil, fmt.Errorf("store.SelectPastSessions: scan: %w", err)
		}
		ps.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
		out = append(out, ps)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.SelectPastSessions: rows: %w", err)
	}

	for i := range out {
		msgs, err := topSessionErrors(ctx, db, out[i].SessionID)
		if err != nil {
			return nil, fmt.Errorf("store.SelectPastSessions: errors for %s: %w", out[i].SessionID, err)
		}
		out[i].ErrorMessages = msgs
	}
	return out, nil
}

// topSessionErrors returns the top recurring error messages for one session
// (grouped, count-descending) so the ask-your-past prompt can answer "what
// kept failing" questions without shipping raw transcripts.
func topSessionErrors(ctx context.Context, db *sql.DB, sessionID string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
SELECT error_message
FROM api_turns
WHERE session_id = ? AND error_message IS NOT NULL AND error_message != ''
GROUP BY error_message
ORDER BY COUNT(*) DESC, error_message
LIMIT 3`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
