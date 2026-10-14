-- 163_plan_limits.sql - the "Plans & limits" module (docs/plans-and-limits.md,
-- operator rulings D-640 / D-642): how much of each AI plan and API balance
-- the developer has left.
--
-- NODE-LOCAL. None of these tables ever reaches the org push wire: there is no
-- share key, no orgpush.go reference and no paired server migration
-- (pinned by tests/invariant/privacy_test.go's forbidden-table sentinel).
-- Credentials are NOT here: API keys live in the OS keychain
-- (internal/keystore), and a borrowed vendor token is held in memory for one
-- request only. plan_accounts.secret_keys names WHICH secrets are stored,
-- never a value.
--
-- One owner: internal/store/planlimits.go is the only writer of all four.
-- The passive signals the module reads (limit_snapshots, the codex
-- rate_limit actions, tool_account_observations) are read-only here; the
-- account-scope binding for limit_snapshots is a READ-TIME rule
-- (planlimits.ResolveScope), so limit_snapshots keeps its single proxy writer.

-- plan_accounts: one row per account the user added. params is a JSON object
-- of NON-secret parameters (team id, username, region, manual plan facts).
CREATE TABLE IF NOT EXISTS plan_accounts (
    id             TEXT PRIMARY KEY,
    provider       TEXT NOT NULL,
    source         TEXT NOT NULL,
    label          TEXT NOT NULL DEFAULT '',
    params         TEXT NOT NULL DEFAULT '{}',
    secret_keys    TEXT NOT NULL DEFAULT '',  -- comma list of stored secret NAMES
    enabled        INTEGER NOT NULL DEFAULT 1,
    created_at     INTEGER NOT NULL,          -- unix seconds
    updated_at     INTEGER NOT NULL,
    run_status     TEXT NOT NULL DEFAULT '',  -- a planlimits status code
    failures       INTEGER NOT NULL DEFAULT 0,
    shape_failures INTEGER NOT NULL DEFAULT 0,
    next_due       INTEGER NOT NULL DEFAULT 0,
    stopped        INTEGER NOT NULL DEFAULT 0,
    last_run_at    INTEGER NOT NULL DEFAULT 0
);

-- plan_snapshots: one row per successful observation. account_id is NULL for
-- the Claude Code status line feed (a passive source with no account), whose
-- rows carry the session id for the read-time account binding. body is the
-- normalized planlimits.Snapshot as JSON (numbers only; a missing number is
-- absent, never zero).
CREATE TABLE IF NOT EXISTS plan_snapshots (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id  TEXT,
    provider    TEXT NOT NULL,
    source      TEXT NOT NULL,
    session_id  TEXT,
    observed_at INTEGER NOT NULL,
    status      TEXT NOT NULL,
    body        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_plan_snapshots_account ON plan_snapshots(account_id, observed_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_plan_snapshots_source ON plan_snapshots(source, observed_at DESC);

-- plan_runs: one row per poll attempt, codes only: no request, no header, no
-- body. detail is a scrubbed, capped error class.
CREATE TABLE IF NOT EXISTS plan_runs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id  TEXT NOT NULL,
    started_at  INTEGER NOT NULL,
    ended_at    INTEGER NOT NULL,
    outcome     TEXT NOT NULL,
    detail      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_plan_runs_account ON plan_runs(account_id, started_at DESC);

-- plan_optins: the per-provider experimental switch (rung 2). Absent = off.
CREATE TABLE IF NOT EXISTS plan_optins (
    provider   TEXT PRIMARY KEY,
    enabled_at INTEGER NOT NULL
);
