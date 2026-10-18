-- 171_action_fk_child_indexes.sql - index every child column that references
-- actions(id), so deleting an action does not scan whole tables.
--
-- The node runs with foreign_keys ON. Deleting a parent row makes SQLite look
-- for children in every table that references it; with no index on the child
-- column that is a FULL SCAN of the child table PER DELETED ACTION. On the
-- operator's node (2026-10-18) guard_events held 6M+ rows with no index on
-- action_id, so one 1,000-action retention batch meant ~6 billion row checks
-- and a month's prune ran for hours while holding the write lock: ingestion
-- stalled on SQLITE_BUSY after every restart. retrieval_signals and
-- process_runs already had these indexes (014 / 044); the four below did not.
-- compaction_events.session_id (FK to sessions, which the orphaned-session
-- sweep deletes from) had the same gap; TestRetentionParentsHaveIndexedChildren
-- (internal/db) now pins every child of a retention parent.
--
-- NODE-LOCAL indexes only: no column or wire change, no server pair.
CREATE INDEX IF NOT EXISTS idx_guard_events_action      ON guard_events(action_id);
CREATE INDEX IF NOT EXISTS idx_failure_context_action   ON failure_context(action_id);
CREATE INDEX IF NOT EXISTS idx_file_state_last_action   ON file_state(last_action_id);
CREATE INDEX IF NOT EXISTS idx_process_events_action    ON process_events(action_id);
CREATE INDEX IF NOT EXISTS idx_compaction_events_session ON compaction_events(session_id);
