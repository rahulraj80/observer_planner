-- 171_telemetry_staging.sql — telemetry rail W2, the CLIENT staging table
-- (telemetry-rail plan §D2/D5; canonical plan in observer_reporter). One row
-- per contract event waiting for the batcher. BOUNDED: the retention DELETE
-- below (enforced on insert by internal/telemetry.Collector) keeps at most
-- MaxStagedEvents rows / StagedMaxAge — drop-OLDEST under pressure, with the
-- number of dropped events counted into telemetry_state.dropped_count so the
-- gap is reported honestly (batch_meta) rather than silently lost.
--
-- Consent semantics: rows are written ONLY after the tier gate admits the
-- event (contract.TierAllows); a tier downgrade purges staged rows the new
-- tier may no longer emit (internal/telemetry.PurgeAboveTier) — deep rows
-- never outlive their consent.
--
-- Content-free by construction (ruling A2): the payload column holds the
-- contract Event.fields JSON — typed enums, integers, and for the deep tier a
-- scrubbed, length-capped argv string. No free text, no paths, no message
-- bodies. install_id/seq make each row uniquely addressable for the
-- batcher's seq windows.
CREATE TABLE IF NOT EXISTS telemetry_staged (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    install_id  TEXT NOT NULL,
    seq         INTEGER NOT NULL,
    event       TEXT NOT NULL,
    payload     TEXT NOT NULL,
    bytes       INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL,
    UNIQUE(install_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_telemetry_staged_created ON telemetry_staged(created_at);

-- Singleton state row (CHECK id = 1, same shape as org_enrolment): the
-- install identity, the monotonic per-install event seq, and the honest-gap
-- counters. install_id is random (never hardware-derived — plan §D2), minted
-- lazily by the collector on first use; NULL until then.
CREATE TABLE IF NOT EXISTS telemetry_state (
    id             INTEGER PRIMARY KEY CHECK (id = 1),
    install_id     TEXT,
    next_seq       INTEGER NOT NULL DEFAULT 1,
    dropped_count  INTEGER NOT NULL DEFAULT 0,
    last_ack_seq   INTEGER NOT NULL DEFAULT 0,
    last_batch_at  TEXT NOT NULL DEFAULT ''
);
INSERT INTO telemetry_state (id) SELECT 1 WHERE NOT EXISTS (SELECT 1 FROM telemetry_state WHERE id = 1);
