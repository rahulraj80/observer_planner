-- 158_component_purge_plans.sql - persisted component purge plans (modular
-- packaging plan section 3.2.1 and 6.3, phase P1, lane MP-GATE; the next
-- contiguous agent number per the migration steward, D-417).
--
-- WHY. Removing a component keeps its data (operator ruling Q7). Deleting a
-- removed component's rows is a separate, explicit action: a preview builds
-- the exact candidate set and persists it here as an immutable, expiring
-- plan; the confirmation names the plan id and deletes ONLY those primary
-- keys in one short transaction, re-checking each row still matches the
-- component's selector (rows that no longer match are skipped and
-- reported). No transaction is held open across the human confirmation.
--
-- STATE MACHINE (internal/store/componentpurge.go, one table-driven
-- transition set): interactive plans go previewed -> consumed | expired |
-- cancelled (15-minute expiry); scheduled plans go grace_pending ->
-- eligible -> consumed, or cancelled / expired, with grace_until the end of
-- the grace period and expires_at 24 hours after it. Consumption is
-- one-shot.
--
-- NODE-LOCAL. Neither table is ever on the org-push wire (forbidden-table
-- sentinel in tests/invariant/privacy_test.go). A purge always runs under
-- db.WithRetentionDeletes, so it emits no org deletion tombstone: rows
-- already pushed follow the org's retention, never the node's purge. No
-- server pair.

CREATE TABLE IF NOT EXISTS component_purge_plans (
    id              TEXT PRIMARY KEY,
    kind            TEXT NOT NULL CHECK (kind IN ('interactive', 'scheduled')),
    component       TEXT NOT NULL,
    catalog_version INTEGER NOT NULL,
    policy_version  TEXT NOT NULL DEFAULT '',
    state           TEXT NOT NULL CHECK (state IN ('previewed', 'grace_pending', 'eligible', 'consumed', 'expired', 'cancelled')),
    created_at      TEXT NOT NULL,
    grace_until     TEXT,
    expires_at      TEXT NOT NULL,
    consumed_at     TEXT,
    counts_json     TEXT NOT NULL DEFAULT '{}',
    result_json     TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_component_purge_plans_state ON component_purge_plans(state, expires_at);

CREATE TABLE IF NOT EXISTS component_purge_candidates (
    plan_id TEXT NOT NULL REFERENCES component_purge_plans(id) ON DELETE CASCADE,
    tbl     TEXT NOT NULL,
    pk      TEXT NOT NULL,
    PRIMARY KEY (plan_id, tbl, pk)
) WITHOUT ROWID;
