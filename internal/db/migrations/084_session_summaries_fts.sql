-- Migration 084: FTS5 index over sessions.summary_md for the "ask your past"
-- surface (Phase D Stage 1 item 4): BM25-ranked retrieval of the user's own
-- persisted, scrubbed session summaries.
--
-- sessions.id is a TEXT primary key, but the table is a normal rowid table,
-- so we key the index on sessions.rowid: triggers keep it in sync with
-- sessions.summary_md, and the one-time backfill below covers pre-existing
-- rows. summary_md is content-bearing by design (this surface answers "what
-- did I do last Tuesday"), but it is the user's own node-local data: this
-- table is never selected by SelectUnpushedSince, and the ask-your-past
-- prompt builder additionally reduces project roots to a bare <repo> marker
-- before any (opt-in, guarded) egress.

CREATE VIRTUAL TABLE IF NOT EXISTS session_summaries_fts USING fts5(
    session_id UNINDEXED,
    summary_md,
    tokenize = 'unicode61 remove_diacritics 2'
);

INSERT INTO session_summaries_fts(rowid, session_id, summary_md)
SELECT rowid, id, summary_md FROM sessions
WHERE summary_md IS NOT NULL AND summary_md != '';

CREATE TRIGGER IF NOT EXISTS session_summaries_fts_insert
AFTER INSERT ON sessions
WHEN NEW.summary_md IS NOT NULL AND NEW.summary_md != ''
BEGIN
    INSERT INTO session_summaries_fts(rowid, session_id, summary_md)
    VALUES (NEW.rowid, NEW.id, NEW.summary_md);
END;

CREATE TRIGGER IF NOT EXISTS session_summaries_fts_delete
AFTER DELETE ON sessions
BEGIN
    DELETE FROM session_summaries_fts WHERE rowid = OLD.rowid;
END;

CREATE TRIGGER IF NOT EXISTS session_summaries_fts_update
AFTER UPDATE OF summary_md ON sessions
BEGIN
    DELETE FROM session_summaries_fts WHERE rowid = OLD.rowid;
    INSERT INTO session_summaries_fts(rowid, session_id, summary_md)
    SELECT NEW.rowid, NEW.id, NEW.summary_md
    WHERE NEW.summary_md IS NOT NULL AND NEW.summary_md != '';
END;
