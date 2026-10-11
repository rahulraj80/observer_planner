-- 153_org_intel_consent.sql -- the NODE half of the Cloud Intelligence
-- parity consent model (docs/plans/cloud-intelligence-parity-design-
-- 2026-10-03.md section 3.7, operator rulings R1/R2; paired with org-server
-- migration 197 / PostgreSQL 0064, which add the requirement settings and the
-- server's record of each member node's own report).
--
-- Three NODE-LOCAL tables. None of them is ever pushed (all three names are in
-- the tests/invariant/privacy_test.go forbidden set); the server learns a
-- member's consent only from the node's own signed POST
-- /api/agent/intel/consent report, never from the push wire.
--
-- org_intel_requirement: single-row cache of the newest VERIFIED signed
--   requirement document (GET /api/agent/intel/requirement), the
--   org_display_policy (148) shape. A refused, unsigned or 404 fetch never
--   touches the row; `observer unenroll` clears it. It is NOT a grant
--   replacement and is never auto-accepted: on an individual-tenancy node a
--   requirement the developer has not accepted is only PENDING.
--
-- org_intel_consent: append-only consent events ('accept' | 'withdraw' |
--   'leave'), written ONLY by the interactive accept path (dashboard consent
--   screen, `observer intel accept`, the enrolment consent step) and by
--   withdraw / leave. No org document can create a row (INV-13). An accept row
--   records exactly what the developer was shown: the requirement version, the
--   evidence level and purposes it covers, and the SHA-256 of the one
--   disclosure text (orgcontract.IntelDisclosure). The current state for an
--   org is its latest row.
--
-- org_intel_receipts: append-only LOCAL audit of every other org-plane
--   event the developer may want to see later: a managed-node notice shown, a
--   T-155d sweep of cached results pulled without consent, the cache cleared
--   on leave, a consent report the org server refused. Content-free: kind,
--   org id, counts and enum detail only.
CREATE TABLE IF NOT EXISTS org_intel_requirement (
    id            INTEGER PRIMARY KEY CHECK (id = 1),
    org_id        TEXT NOT NULL,
    version       INTEGER NOT NULL,
    body          TEXT NOT NULL,
    body_hash     TEXT NOT NULL,
    signature     TEXT NOT NULL,
    server_pubkey TEXT NOT NULL,
    received_at   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS org_intel_consent (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id              TEXT NOT NULL,
    event               TEXT NOT NULL CHECK (event IN ('accept', 'withdraw', 'leave')),
    requirement_version INTEGER NOT NULL DEFAULT 0,
    evidence_level      TEXT NOT NULL DEFAULT '',
    purposes            TEXT NOT NULL DEFAULT '[]',
    disclosure_digest   TEXT NOT NULL DEFAULT '',
    consent_mode        TEXT NOT NULL DEFAULT '',
    actor               TEXT NOT NULL DEFAULT '',
    created_at          TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_org_intel_consent_org ON org_intel_consent(org_id, id);

CREATE TABLE IF NOT EXISTS org_intel_receipts (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    kind       TEXT NOT NULL,
    org_id     TEXT NOT NULL DEFAULT '',
    count      INTEGER NOT NULL DEFAULT 0,
    detail     TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
