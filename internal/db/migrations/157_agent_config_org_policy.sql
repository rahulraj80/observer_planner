-- 157_agent_config_org_policy.sql -- the organization's AI tool settings
-- policy on a MANAGED node (adapter config phases P6 and P7, backlog item 8;
-- docs/agent-config.md "Organization policy"). NODE-LOCAL; neither table is
-- ever selected by internal/store/orgpush.go (pinned by
-- tests/invariant/privacy_test.go). The applied state reaches the org only
-- through the envelope-level AgentConfigState report, composed by a func
-- seam from the closed fields below (internal/store/agentconfigorgstate.go).
--
-- org_agent_config_policy: the last VERIFIED signed document of
-- GET /api/agent/agent-config-policy (one row). A 404 never clears it; only a
-- signed newer version replaces it (D-016).
--
-- agent_config_org_state: one row per org directive per target file: what
-- the node did (agentconfigorg.State), the org value's hash, what it found,
-- the drift events within the back-off window (D-101) and when. project_root
-- is a local path and never leaves the node; project_hash is the identity the
-- org targeted.
CREATE TABLE IF NOT EXISTS org_agent_config_policy (
    id            INTEGER PRIMARY KEY CHECK (id = 1),
    org_id        TEXT NOT NULL,
    subject       TEXT NOT NULL,
    version       INTEGER NOT NULL,
    body          TEXT NOT NULL,
    body_hash     TEXT NOT NULL,
    signature     TEXT NOT NULL,
    server_pubkey TEXT NOT NULL,
    received_at   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_config_org_state (
    setting_id     TEXT NOT NULL,
    file_id        TEXT NOT NULL,
    project_root   TEXT NOT NULL DEFAULT '',
    project_hash   TEXT NOT NULL DEFAULT '',
    tool           TEXT NOT NULL,
    scope          TEXT NOT NULL DEFAULT '',
    mode           TEXT NOT NULL DEFAULT '',
    layer          TEXT NOT NULL DEFAULT '',
    policy_name    TEXT NOT NULL DEFAULT '',
    state          TEXT NOT NULL,
    reason         TEXT NOT NULL DEFAULT '',
    expected_hash  TEXT NOT NULL DEFAULT '',
    observed_hash  TEXT NOT NULL DEFAULT '',
    observed_value TEXT,
    observed_secret INTEGER NOT NULL DEFAULT 0,
    drift_times    TEXT NOT NULL DEFAULT '[]',
    last_applied_at TEXT NOT NULL DEFAULT '',
    last_drift_at  TEXT NOT NULL DEFAULT '',
    policy_version INTEGER NOT NULL DEFAULT 0,
    observed_at    TEXT NOT NULL,
    PRIMARY KEY (setting_id, file_id, project_root)
);
