-- 151_model_session_indexes.sql — model-leading read indexes for the
-- session-grain model filter (Sol review 2026-10, finding A3-07).
--
-- WHY. Every session-grain model filter on the node dashboard (Sessions,
-- Actions, Projects, Discover, Learn, the advisor, ...) resolves "the
-- sessions that used model X" through internal/modelfilter's subquery:
--   SELECT session_id FROM token_usage WHERE model = ? AND session_id IS NOT NULL
--   UNION SELECT session_id FROM api_turns WHERE model = ? AND session_id IS NOT NULL
-- Neither table had an index that leads with model, so each arm was a FULL
-- scan of the table on every filtered request (often several times per page:
-- count, page, aggregates), whatever the request's window.
--
-- MEASURED (modernc.org/sqlite, the daemon's own driver; a seeded node DB of
-- 20k sessions over 365 days, 1.0M token_usage + 300k api_turns rows, 42
-- models with ~30% of rows on one model; median of 7, warm cache):
--   rare model (0.5% of sessions), 7-day session count:   587 ms -> 7 ms
--   rare model, all-time session count:                    607 ms -> 8 ms
--   popular model (~30%), 7-day session count:             964 ms -> 181 ms
--   popular model, all-time session count:                 875 ms -> 153 ms
-- EXPLAIN QUERY PLAN goes from `SCAN token_usage` / `SCAN api_turns` to
-- `SEARCH ... USING COVERING INDEX (model=?)` on both arms. Cost: 24.6 MB on
-- that 363 MB DB (~7%), one more index write per usage row, a one-off build
-- of ~7 s at that size.
--
-- Index-only: no column, no table, no wire-shape change, both tables'
-- existing indexes kept. The org server carries its own owner-aware twin
-- (server migration 195 / PostgreSQL 0061, (model, user_id, session_id));
-- this node index is not on the push wire and needs no privacy-sentinel change.
CREATE INDEX IF NOT EXISTS idx_token_usage_model_session
    ON token_usage(model, session_id);

CREATE INDEX IF NOT EXISTS idx_api_turns_model_session
    ON api_turns(model, session_id);
