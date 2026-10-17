-- 154_org_intel_outbox.sql - Cloud Intelligence parity, phase 2: the node's
-- org-plane submission outbox (design
-- docs/plans/cloud-intelligence-parity-design-2026-10-03.md section 4.1, the
-- B1 rail).
--
-- An enrolled node builds the org-plane evidence envelope itself
-- (cloudevidence, PlaneOrg) and submits the exact preview==upload bytes to its
-- org server over the signed POST /api/agent/intel/evidence rail. One row per
-- submission:
--
--   envelope        the exact bytes, held only until the submission reaches a
--                   terminal state (then cleared; bytes keeps the size for the
--                   ledger).
--   state           pending | failed_retryable (sendable, after next_at) |
--                   submitted | duplicate | already_enriched | refused
--                   (terminal).
--   reason          the server's closed refusal code, or a local note.
--   trigger         background (the daemon's own sweep) or developer
--                   (Enrich now / re-enrich).
--
-- NODE-LOCAL: the table name is in the privacy sentinel's forbidden set, so it
-- can never enter the push envelope (the evidence leaves only through the
-- dedicated rail). Leaving the organization clears it.
CREATE TABLE IF NOT EXISTS org_intel_outbox (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id          TEXT NOT NULL,
    session_id      TEXT NOT NULL,
    purpose         TEXT NOT NULL,
    evidence_level  TEXT NOT NULL,
    trigger_kind    TEXT NOT NULL CHECK (trigger_kind IN ('background', 'developer')),
    evidence_digest TEXT NOT NULL,
    upload_digest   TEXT NOT NULL,
    envelope        TEXT NOT NULL DEFAULT '',
    bytes           INTEGER NOT NULL DEFAULT 0,
    state           TEXT NOT NULL DEFAULT 'pending'
                    CHECK (state IN ('pending', 'failed_retryable', 'submitted', 'duplicate', 'already_enriched', 'refused')),
    reason          TEXT NOT NULL DEFAULT '',
    job_id          INTEGER NOT NULL DEFAULT 0,
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_at         TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    UNIQUE (org_id, session_id, purpose, upload_digest)
);

CREATE INDEX IF NOT EXISTS idx_org_intel_outbox_state ON org_intel_outbox (org_id, state, next_at);
CREATE INDEX IF NOT EXISTS idx_org_intel_outbox_session ON org_intel_outbox (session_id, id);
