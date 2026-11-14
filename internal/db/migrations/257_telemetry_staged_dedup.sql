-- 172_telemetry_staged_dedup.sql — telemetry rail W2.1, cross-pass derive
-- dedup made durable. The W2 Derive deduplicated re-derives against
-- the payload's local "k" key, but stageEvent strips "k" from the wire
-- payload (it is not contract vocabulary), so the map never matched and
-- every pass re-staged every candidate — seqs grew without bound and, once
-- the ack watermark landed, acked-but-restaged rows re-sent forever.
--
-- The dedup key is LOCAL identity metadata (same content-free family as the
-- rest of the row: deterministic strings derived from local ids and day
-- buckets, never free text). It rides in its own column so the wire payload
-- stays exactly the contract Event.fields and Derive can cheaply load the
-- already-staged key set for its lookback window.
--
ALTER TABLE telemetry_staged ADD COLUMN dedup_key TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_telemetry_staged_dedup
    ON telemetry_staged(install_id, event, dedup_key);
