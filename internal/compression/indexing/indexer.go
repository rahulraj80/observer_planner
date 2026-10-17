package indexing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// DefaultMaxExcerptBytes is the default cap for stored excerpts.
const DefaultMaxExcerptBytes = 2048

// Indexer records tool output excerpts in the FTS5 action_excerpts virtual
// table so the MCP server's search_past_outputs tool can retrieve them.
//
// The zero value is unusable — call New.
type Indexer struct {
	db              *sql.DB
	maxExcerptBytes int
}

// New returns an Indexer bound to db. maxExcerptBytes ≤ 0 falls back to
// DefaultMaxExcerptBytes.
func New(db *sql.DB, maxExcerptBytes int) *Indexer {
	if maxExcerptBytes <= 0 {
		maxExcerptBytes = DefaultMaxExcerptBytes
	}
	return &Indexer{db: db, maxExcerptBytes: maxExcerptBytes}
}

// deletePriorSQL removes every excerpt row of one action.
//
// action_id is UNINDEXED in the FTS5 table (001_initial.sql), so the
// obvious `DELETE FROM action_excerpts WHERE action_id = ?` is planned as
// `SCAN action_excerpts VIRTUAL TABLE INDEX 0:`: a read of EVERY stored
// excerpt, under the write lock, once per indexed tool result. On a node
// with ~500k excerpts that was 0.4 s in sqlite3 and about 1 s in modernc
// per call, which stalled watcher ingestion for days (T-412).
//
// FTS5 keeps its stored columns in the ordinary shadow table
// action_excerpts_content (c0 is action_id), and migration 147 indexes c0.
// Resolving the action id to FTS rowids through that index and deleting by
// rowid equality is two B-tree seeks. It finds every row whatever rowid it
// was given, so legacy rows (auto-assigned rowids) and new rows behave the
// same and no re-key or rebuild is needed. The equality comparison is the
// one the old statement made (both columns carry no affinity), so the set of
// rows deleted is unchanged. Pinned by TestDeletePriorUsesRowidSeek.
const deletePriorSQL = `DELETE FROM action_excerpts
 WHERE rowid IN (SELECT id FROM action_excerpts_content WHERE c0 = ?)`

const insertExcerptSQL = `INSERT INTO action_excerpts (action_id, tool_name, target, excerpt, error_message)
 VALUES (?, ?, ?, ?, ?)`

// Index records an excerpt for an action. The stored excerpt is the first
// half of maxExcerptBytes plus the last half when raw exceeds the cap, joined
// by a "…" marker — mirroring the spec's "first 1KB + last 1KB" rule.
//
// Idempotent: re-indexing the same action_id overwrites the prior excerpt.
// The delete and the insert run in one transaction, so a reader never sees
// the action with no excerpt or with two.
func (i *Indexer) Index(ctx context.Context, actionID int64, toolName, target, raw, errorMessage string) error {
	return i.index(ctx, actionID, toolName, target, raw, errorMessage, true)
}

// IndexNew records the excerpt of an action row that was INSERTED (not
// upserted) by the caller's current batch, skipping the prior-row delete.
// actions.id is AUTOINCREMENT, so an id allocated by this insert has never
// been used before and no excerpt can carry it yet. Callers that cannot
// prove a fresh insert (cross-tick outcome updates, hooks, backfills,
// imports) must use Index.
func (i *Indexer) IndexNew(ctx context.Context, actionID int64, toolName, target, raw, errorMessage string) error {
	return i.index(ctx, actionID, toolName, target, raw, errorMessage, false)
}

func (i *Indexer) index(ctx context.Context, actionID int64, toolName, target, raw, errorMessage string, replace bool) error {
	if actionID == 0 {
		return errors.New("indexing.Index: actionID required")
	}
	excerpt := TrimExcerpt(raw, i.maxExcerptBytes)
	if !replace {
		if _, err := i.db.ExecContext(ctx, insertExcerptSQL,
			actionID, toolName, target, excerpt, errorMessage); err != nil {
			return fmt.Errorf("indexing.Index: insert: %w", err)
		}
		return nil
	}
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("indexing.Index: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// Replace any prior row with the same action_id so re-parsing doesn't
	// multiply entries.
	if _, err := tx.ExecContext(ctx, deletePriorSQL, actionID); err != nil {
		return fmt.Errorf("indexing.Index: delete prior: %w", err)
	}
	if _, err := tx.ExecContext(ctx, insertExcerptSQL,
		actionID, toolName, target, excerpt, errorMessage); err != nil {
		return fmt.Errorf("indexing.Index: insert: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("indexing.Index: commit: %w", err)
	}
	return nil
}

// SearchResult is a single FTS5 match returned by Search.
type SearchResult struct {
	ActionID     int64
	ToolName     string
	Target       string
	Excerpt      string
	ErrorMessage string
	Rank         float64
	// Snippet is the FTS5 snippet() rendering of the best-matching
	// column — the match region bracketed by the marks passed to
	// SearchWithSnippets. Empty for plain Search calls.
	Snippet string
}

// Search runs an FTS5 MATCH query against action_excerpts. query uses FTS5
// syntax — multi-token queries should use FTS5 phrase/boolean syntax
// directly; single-token queries with special characters (`.`, `:`, `(`, `)`,
// `*`, `-`) are auto-quoted by [sanitizeFTSQuery] so common AI-tool inputs
// like `app.set` don't trip FTS5's tokenizer (which would otherwise return
// `syntax error near "."`). Results are ordered by rank (ascending; more
// negative = more relevant in SQLite FTS5).
func (i *Indexer) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 50
	}
	query = sanitizeFTSQuery(query)
	rows, err := i.db.QueryContext(
		ctx,
		`SELECT action_id, tool_name, target, excerpt, error_message, rank
		 FROM action_excerpts
		 WHERE action_excerpts MATCH ?
		 ORDER BY rank
		 LIMIT ?`,
		query, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("indexing.Search: query: %w", err)
	}
	defer rows.Close()
	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.ActionID, &r.ToolName, &r.Target, &r.Excerpt, &r.ErrorMessage, &r.Rank); err != nil {
			return nil, fmt.Errorf("indexing.Search: scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SearchWithSnippets is Search plus an FTS5 snippet() column: the
// match region of the best-matching column, bracketed by startMark /
// endMark, capped at 16 tokens with "…" ellipses. Marks should be
// non-text sentinels the caller splits on (e.g. "\x01"/"\x02" mapped
// to <mark> client-side) — never raw HTML, so a stored excerpt can't
// smuggle markup through the result. Same sanitization, ordering, and
// limit semantics as Search.
func (i *Indexer) SearchWithSnippets(ctx context.Context, query string, limit int, startMark, endMark string) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 50
	}
	query = sanitizeFTSQuery(query)
	rows, err := i.db.QueryContext(
		ctx,
		`SELECT action_id, tool_name, target, excerpt, error_message, rank,
		        snippet(action_excerpts, -1, ?, ?, '…', 16)
		 FROM action_excerpts
		 WHERE action_excerpts MATCH ?
		 ORDER BY rank
		 LIMIT ?`,
		startMark, endMark, query, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("indexing.SearchWithSnippets: query: %w", err)
	}
	defer rows.Close()
	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.ActionID, &r.ToolName, &r.Target, &r.Excerpt, &r.ErrorMessage, &r.Rank, &r.Snippet); err != nil {
			return nil, fmt.Errorf("indexing.SearchWithSnippets: scan: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// sanitizeFTSQuery turns a user-facing query into a form that FTS5's
// MATCH operator accepts without throwing on common AI-tool inputs.
//
// FTS5 treats `.`, `:`, `(`, `)`, `*`, and `-` as syntactic. A naive
// pass-through of `app.set` produces `syntax error near "."` because
// FTS5 parses it as `<column>.<value>`. The fix: when the query is a
// single token (no whitespace) AND contains any of those special
// chars AND isn't already quoted, wrap it in double quotes so FTS5
// treats it as a literal phrase.
//
// Multi-token queries pass through untouched — those callers are
// presumed to be using FTS5 boolean / phrase syntax deliberately
// (e.g. `"app.set" OR "app.use"` from a power user). Already-quoted
// queries pass through too. This is a least-surprise default: simple
// inputs work, advanced syntax stays available.
func sanitizeFTSQuery(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return q
	}
	// Multi-token: trust the caller (FTS5 syntax in play).
	if strings.ContainsAny(q, " \t\n") {
		return q
	}
	// Already quoted: trust the caller.
	if strings.HasPrefix(q, `"`) {
		return q
	}
	// Single token with FTS5-special chars: wrap as phrase. Embedded
	// double quotes are escaped by FTS5's standard `""` doubling.
	if strings.ContainsAny(q, `.:()*-`) {
		return `"` + strings.ReplaceAll(q, `"`, `""`) + `"`
	}
	return q
}

// TrimExcerpt reduces raw to at most maxBytes by keeping the first and last
// halves separated by a "…[truncated]…" marker when the input exceeds the
// cap. Short inputs are returned unchanged.
func TrimExcerpt(raw string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxExcerptBytes
	}
	if len(raw) <= maxBytes {
		return raw
	}
	const marker = "…[truncated]…"
	budget := maxBytes - len(marker)
	if budget <= 0 {
		return raw[:maxBytes]
	}
	head := budget / 2
	tail := budget - head
	return raw[:head] + marker + raw[len(raw)-tail:]
}
