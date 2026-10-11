-- 152_access_grants.sql - the node half of the request/grant flow
-- (docs/plans/request-grants-design-2026-10-03.md §4.1, D-080..D-095).
--
-- Three NODE-LOCAL tables (no server pair; pinned by the privacy sentinel in
-- tests/invariant/privacy_test.go) plus one wire column.
--
-- org_access_grant_state: the single-row record of the last VERIFIED signed
-- grants document (GET /api/agent/grants, domain sbo-access-grant-v1) and
-- the receipt facts that bound a grant the node can no longer re-verify:
-- the server clock the body was built at, the node clock it arrived at, the
-- last verified sync, the offline window the org allows, and the highest
-- node clock ever seen (a clock set back reads as that mark, GRANT-3).
--
-- org_access_grants: the decoded grants of that document. Replaced WHOLE,
-- in one transaction, by a newer verified document only; only a signed body
-- with an empty list empties it. uses_used is the local use counter a guard
-- consumer (design §3.2, phase G3) decrements and survives a replace for a
-- grant id still present.
--
-- access_request_outbox: a request the developer raised while the org was
-- unreachable, kept with the exact bytes they previewed and posted on the
-- next push cycle. The developer sees it as "queued".
--
-- guard_events.grant_id: the org grant that covered an event (set by the
-- guard consumer, G3). It ships as metadata on the existing push (org 196 /
-- pg 0062 carry the paired column); a NULL is "no grant".

CREATE TABLE IF NOT EXISTS org_access_grant_state (
    id                INTEGER PRIMARY KEY CHECK (id = 1),
    org_id            TEXT    NOT NULL,
    subject           TEXT    NOT NULL,
    version           INTEGER NOT NULL,
    body              TEXT    NOT NULL,
    body_hash         TEXT    NOT NULL,
    signature         TEXT    NOT NULL,
    server_pubkey     TEXT    NOT NULL,
    server_now_ns     INTEGER NOT NULL,
    received_at_ns    INTEGER NOT NULL,
    last_verified_ns  INTEGER NOT NULL,
    high_water_ns     INTEGER NOT NULL DEFAULT 0,
    max_offline_s     INTEGER NOT NULL DEFAULT 0,
    issued_at         TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS org_access_grants (
    grant_id          INTEGER PRIMARY KEY,
    type              TEXT    NOT NULL,
    effect            TEXT    NOT NULL DEFAULT '',
    subject_kind      TEXT    NOT NULL DEFAULT 'member',
    machine_identity  TEXT    NOT NULL DEFAULT '',
    rule_id           TEXT    NOT NULL DEFAULT '',
    lane              TEXT    NOT NULL DEFAULT '',
    command_class     TEXT    NOT NULL DEFAULT '',
    operand_hash      TEXT    NOT NULL DEFAULT '',
    project_root_hash TEXT    NOT NULL DEFAULT '',
    target            TEXT    NOT NULL DEFAULT '',
    starts_at_ns      INTEGER NOT NULL,
    expires_at_ns     INTEGER NOT NULL DEFAULT 0,
    max_uses          INTEGER NOT NULL DEFAULT 0,
    uses_used         INTEGER NOT NULL DEFAULT 0,
    request_id        INTEGER NOT NULL DEFAULT 0,
    doc_version       INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_org_access_grants_rule ON org_access_grants(rule_id);

CREATE TABLE IF NOT EXISTS access_request_outbox (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id        TEXT    NOT NULL,
    request_json  TEXT    NOT NULL,
    created_at    TEXT    NOT NULL,
    attempts      INTEGER NOT NULL DEFAULT 0,
    last_error    TEXT    NOT NULL DEFAULT '',
    last_attempt  TEXT    NOT NULL DEFAULT ''
);

ALTER TABLE guard_events ADD COLUMN grant_id INTEGER;
