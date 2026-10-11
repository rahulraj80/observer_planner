-- 155_agent_config_layers.sql: the node-local layer store of the adapter
-- config editor (backlog items 2 and 7, phase P5;
-- docs/plans/adapter-config-and-common-vocabulary-plan-2026-10-03.md
-- sections 3 and 5.2 step 10).
--
-- agent_config_layers: one row per vendor-file key Observer's editor wrote,
-- naming the developer layer that authored it (l1_all_adapters for the
-- common form, l2_adapter for a tool's user file, l3_project for a project
-- file). The editor's layer badge says L1 only while the file still holds
-- exactly the value the L1 save wrote (value_hash, a fingerprint); a value
-- changed outside Observer is adopted into the layer that owns its file.
-- value_json carries the authored value for display and is NULL for a
-- secret (a secret's value is never stored anywhere by Observer).
--
-- agent_config_writes: the developer-visible history of every save (which
-- file, which keys BY NAME, the backup it left, before / after file
-- fingerprints). Never a value.
--
-- NODE-LOCAL: both tables describe this machine's own vendor files. They
-- are never selected by internal/store/orgpush.go (pinned in the
-- forbidden-table sentinel, tests/invariant/privacy_test.go); managed-node
-- reporting (phase P7) composes through a store FUNCTION seam, never these
-- names. No paired server migration. The single writer is
-- internal/store/agentconfig.go. Timestamps are RFC3339 UTC strings.

CREATE TABLE IF NOT EXISTS agent_config_layers (
    id            INTEGER PRIMARY KEY,
    file_path     TEXT NOT NULL,
    key_path      TEXT NOT NULL,
    tool          TEXT NOT NULL,
    setting_id    TEXT NOT NULL DEFAULT '',
    layer         TEXT NOT NULL,
    common_key    TEXT NOT NULL DEFAULT '',
    project_root  TEXT NOT NULL DEFAULT '',
    value_hash    TEXT NOT NULL,
    value_json    TEXT,
    updated_at    TEXT NOT NULL,
    UNIQUE (file_path, key_path)
);

CREATE INDEX IF NOT EXISTS idx_agent_config_layers_tool
    ON agent_config_layers(tool);

CREATE TABLE IF NOT EXISTS agent_config_writes (
    id             INTEGER PRIMARY KEY,
    at             TEXT NOT NULL,
    tool           TEXT NOT NULL,
    file_id        TEXT NOT NULL,
    file_path      TEXT NOT NULL,
    layer          TEXT NOT NULL,
    common_key     TEXT NOT NULL DEFAULT '',
    origin         TEXT NOT NULL,
    mode           TEXT NOT NULL,
    changed_keys   TEXT NOT NULL,
    before_sha256  TEXT NOT NULL DEFAULT '',
    after_sha256   TEXT NOT NULL,
    backup_path    TEXT NOT NULL DEFAULT '',
    created        INTEGER NOT NULL DEFAULT 0,
    warnings       INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_agent_config_writes_at
    ON agent_config_writes(at);
