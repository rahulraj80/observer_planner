-- 166_turn_kind.sql - Requirement-kind axis for api_turns
-- (docs/design/api-intelligence.md §5.1). Persists a requirement-kind per
-- API turn so every cut (dashboard, cost, evidence) can group by "what
-- kind of requirement was this".
--
-- routing already classifies live (routing/turnkind.go, 8 kinds) but only
-- for routed sessions and not persisted against the turn for offline queries.
-- This migration adds the column and an index; the backfill CLI (observer
-- kinds backfill) fills NULL rows deterministically.

ALTER TABLE api_turns ADD COLUMN turn_kind TEXT;

CREATE INDEX IF NOT EXISTS idx_api_turns_turn_kind_ts ON api_turns (turn_kind, timestamp);