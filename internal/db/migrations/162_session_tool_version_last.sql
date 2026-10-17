-- 162_session_tool_version_last.sql - mid-session tool upgrades (T-784
-- part B, operator ruling D-629; paired with server migration 232 / pg 0099).
--
-- WHY. sessions.tool_version (migration 125) is FIRST-WINS-UNLESS-EMPTY: it
-- records the tool build a session STARTED on and never moves. A developer
-- who upgrades the CLI and resumes the same session keeps reporting the old
-- build, so the org "which version is each developer on now" view would be
-- wrong for exactly the people who just upgraded.
--
--   tool_version_last - the highest-ordered valid version observed for the
--                       session (internal/store/toolversion.go::
--                       SetSessionToolVersion, the same ONE write path as
--                       tool_version). tool_version keeps its meaning (the
--                       first build seen); tool_version_last moves forward on
--                       every valid stamp. NULL = no stamp since this
--                       migration; readers fall back to tool_version.
--
-- Nullable, no backfill: an existing session keeps NULL until its transcript
-- is next parsed. The column ships on the org wire as METADATA, exactly like
-- tool_version (SessionRow.ToolVersionLast, omitempty).
--
-- RE-SEND. Migrations 140/142 queue a session for re-push when one of a
-- fixed column list changes. tool_version_last must be on that list or an
-- upgrade stamped after the row first shipped would never reach the org. The
-- trigger is dropped and re-created with the SAME name, the SAME WHEN shape
-- and the migration-142 UPSERT queue insert; only tool_version_last joins the
-- column list and the change predicate (internal/db/triggers_guard_test.go
-- checks the latest definition).
ALTER TABLE sessions ADD COLUMN tool_version_last TEXT;

DROP TRIGGER IF EXISTS org_push_change_sessions;
CREATE TRIGGER org_push_change_sessions
AFTER UPDATE OF
    project_id, tool, model, git_branch, started_at,
    ended_at, total_actions, workspace_hash, workspace, is_worktree,
    surface, surface_host, tool_version, parent_thread_id, tool_version_last
ON sessions
WHEN NEW.rowid > CAST((SELECT value FROM schema_meta WHERE key = 'org_push_floor_sessions') AS INTEGER)
 AND ( OLD.project_id IS NOT NEW.project_id
           OR COALESCE(OLD.tool, '') IS NOT COALESCE(NEW.tool, '')
           OR COALESCE(OLD.model, '') IS NOT COALESCE(NEW.model, '')
           OR COALESCE(OLD.git_branch, '') IS NOT COALESCE(NEW.git_branch, '')
           OR COALESCE(OLD.started_at, '') IS NOT COALESCE(NEW.started_at, '')
           OR COALESCE(OLD.ended_at, '') IS NOT COALESCE(NEW.ended_at, '')
           OR COALESCE(OLD.total_actions, 0) IS NOT COALESCE(NEW.total_actions, 0)
           OR COALESCE(OLD.workspace_hash, '') IS NOT COALESCE(NEW.workspace_hash, '')
           OR COALESCE(OLD.workspace, '') IS NOT COALESCE(NEW.workspace, '')
           OR COALESCE(OLD.is_worktree, 0) IS NOT COALESCE(NEW.is_worktree, 0)
           OR COALESCE(OLD.surface, '') IS NOT COALESCE(NEW.surface, '')
           OR COALESCE(OLD.surface_host, '') IS NOT COALESCE(NEW.surface_host, '')
           OR COALESCE(OLD.tool_version, '') IS NOT COALESCE(NEW.tool_version, '')
           OR COALESCE(OLD.parent_thread_id, '') IS NOT COALESCE(NEW.parent_thread_id, '')
           OR COALESCE(OLD.tool_version_last, '') IS NOT COALESCE(NEW.tool_version_last, '') )
BEGIN
    UPDATE org_push_rev SET rev = rev + 1 WHERE k = 1;
    INSERT INTO org_push_changes (tbl, row_id, seq)
    VALUES ('sessions', NEW.rowid, (SELECT rev FROM org_push_rev WHERE k = 1))
    ON CONFLICT (tbl, row_id) DO UPDATE SET seq = excluded.seq;
END;
