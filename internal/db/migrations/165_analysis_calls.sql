-- 165_analysis_calls.sql - LLM-connection analysis accounting
-- (docs/design/standup-briefing.md §3.6, Phase B). One row per OPTIONAL
-- LLM analysis call (narrative upgrade / briefing Q&A / summaries / triage)
-- made through an [llm.connections] connection. Kept SEPARATE from api_turns
-- so "analysis overhead" is visible and attributable on the cost dashboards
-- without polluting the tool-traffic view (open decision Q2, 2026-08-02).
--
-- NODE-LOCAL + CONTENT-FREE by design: rows carry only ids, counts, and a
-- cost; no prompt or reply text is stored, and the table is NOT read by
-- internal/store/orgpush.go::SelectUnpushedSince (privacy sentinel:
-- tests/invariant/privacy_test.go). No paired orgserver migration.

CREATE TABLE IF NOT EXISTS analysis_calls (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    ts             TEXT    NOT NULL,           -- RFC3339 UTC call time (lexicographically sortable)
    connection_id  TEXT    NOT NULL,           -- llmconn connection id
    model          TEXT    NOT NULL,           -- model used (may be the connection default)
    task           TEXT    NOT NULL,           -- narrative | ask | summarize | triage
    input_tokens   INTEGER NOT NULL DEFAULT 0,
    output_tokens  INTEGER NOT NULL DEFAULT 0,
    cost_usd       REAL    NOT NULL DEFAULT 0, -- list-basis price, cost engine
    latency_ms     INTEGER NOT NULL DEFAULT 0,
    status         TEXT    NOT NULL,           -- ok | error
    error_class    TEXT,                       -- short error class when status = error
    api_turn_id    TEXT                         -- always NULL: never a tool api_turn
);

CREATE INDEX IF NOT EXISTS idx_analysis_calls_ts   ON analysis_calls (ts);
CREATE INDEX IF NOT EXISTS idx_analysis_calls_conn ON analysis_calls (connection_id, ts);
