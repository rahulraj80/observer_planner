-- 148_org_display_policy.sql -- node-side cache of the org's signed DISPLAY
-- POLICY document (GET /api/agent/display-policy; paired with org-server
-- migration 193 / PostgreSQL 0059, which add the setting it carries).
--
-- Operator ruling 2026-10-02: org admins always see dollars; the admin's one
-- choice is whether DEVELOPERS' node dashboards show dollar costs or token
-- usage instead. This row is how an enrolled node knows that choice.
--
-- Single-row cache, the org_announcements (076) shape: the newest VERIFIED
-- document plus the org key it verified under (the cross-rail pin,
-- orgclient/orgpin.go) and the org it was signed for. A refused, unsigned or
-- 404 fetch never touches the row (the node keeps its last verified value);
-- only `observer unenroll` clears it (store.DeleteEnrolment), and the
-- dashboard additionally ignores a row whose org_id is not the CURRENT
-- enrolment's.
--
-- developer_cost_display is the decoded, normalized body value ('usd' |
-- 'tokens'), kept beside the signed body so the dashboard read is one column.
--
-- NODE-LOCAL: RECEIVED state, never pushed (pinned in
-- tests/invariant/privacy_test.go). Pushing it back would tell the server
-- which version a node holds - a receipt nobody asked for.
CREATE TABLE IF NOT EXISTS org_display_policy (
    id                     INTEGER PRIMARY KEY CHECK (id = 1),
    org_id                 TEXT NOT NULL,
    version                INTEGER NOT NULL,
    body                   TEXT NOT NULL,
    body_hash              TEXT NOT NULL,
    signature              TEXT NOT NULL,
    server_pubkey          TEXT NOT NULL,
    developer_cost_display TEXT NOT NULL,
    received_at            TEXT NOT NULL
);
