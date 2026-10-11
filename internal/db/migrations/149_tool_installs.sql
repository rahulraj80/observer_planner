-- 149_tool_installs.sql — the node's record of AI tools the local dashboard
-- itself installed ("Install in terminal" in the New terminal dialog), so the
-- dashboard can offer an Uninstall for exactly those — and only those — tools.
--
-- One row per guided install attempt. The row carries the EXACT argv the
-- daemon executed (argv_json, server-derived from the integration registry,
-- never request input) and its channel (the registry's closed InstallHint
-- vocabulary). The uninstall argv is DERIVED from that recorded argv by the
-- pure internal/toolinstall channel table at uninstall time, and stored here
-- once run (uninstall_argv_json) so the history says exactly what was
-- executed. status is internal/toolinstall's closed lifecycle vocabulary
-- (installing / installed / install_failed / install_interrupted /
-- uninstalling / uninstalled / uninstall_failed / uninstall_interrupted);
-- every status change is a compare-and-set in internal/store/toolinstalls.go,
-- the ONE writer of this table.
--
-- forgotten_at hides a row from every surface without deleting it (the
-- operator's "Forget" for a vendor-script install with no defined uninstall,
-- or a package removed by hand).
--
-- NODE-LOCAL: install records describe this machine's own software, not the
-- org's work. NEVER selected by internal/store/orgpush.go — pinned in the
-- forbidden-table sentinel (tests/invariant/privacy_test.go). No paired
-- server migration. Timestamps are RFC3339 UTC strings like every other
-- table in this schema.

CREATE TABLE IF NOT EXISTS tool_installs (
    id                     INTEGER PRIMARY KEY,
    tool                   TEXT NOT NULL,
    channel                TEXT NOT NULL,
    package                TEXT NOT NULL DEFAULT '',
    argv_json              TEXT NOT NULL,
    display                TEXT NOT NULL,
    version                TEXT NOT NULL DEFAULT '',
    status                 TEXT NOT NULL,
    exit_code              INTEGER,
    started_at             TEXT NOT NULL,
    finished_at            TEXT,
    resolved_bin           TEXT NOT NULL DEFAULT '',
    uninstall_argv_json    TEXT NOT NULL DEFAULT '',
    uninstall_display      TEXT NOT NULL DEFAULT '',
    uninstall_started_at   TEXT,
    uninstall_finished_at  TEXT,
    uninstall_exit_code    INTEGER,
    forgotten_at           TEXT
);

CREATE INDEX IF NOT EXISTS idx_tool_installs_tool
    ON tool_installs(tool, id);
