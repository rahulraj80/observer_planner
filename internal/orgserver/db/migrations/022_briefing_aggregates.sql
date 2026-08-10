-- Daily-Briefing AGGREGATE rollup (docs/design/standup-briefing.md §4, Phase C).
-- Content-free counts + sums pushed by nodes that opted into the node-side
-- [org_client.share] briefing_aggregates toggle. Every key is content-free:
-- day (UTC), tool (adapter name), project_hash (sha256 of the project root —
-- the same posture sessions/api_turns already ship); no prompt, turn, or
-- snapshot content ever crosses the wire, and the org admin cannot force more.
-- The server upserts by the (org_id, user_email, day, tool, project_hash)
-- natural key — windowed-recompute at the node means a re-pushed window is
-- exactly idempotent, so the same bucket may be overwritten as the node's
-- local view of a completed day settles.
CREATE TABLE IF NOT EXISTS briefing_aggregates (
    org_id       TEXT NOT NULL,
    user_email   TEXT NOT NULL,
    day          TEXT NOT NULL, -- UTC YYYY-MM-DD
    tool         TEXT NOT NULL, -- adapter name
    project_hash TEXT NOT NULL DEFAULT '', -- sha256 of project root ('' = none)

    sessions     INTEGER NOT NULL DEFAULT 0,
    turns        INTEGER NOT NULL DEFAULT 0,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cost_usd     REAL    NOT NULL DEFAULT 0,

    updated_at   TEXT NOT NULL,

    PRIMARY KEY (org_id, user_email, day, tool, project_hash)
);
CREATE INDEX IF NOT EXISTS idx_briefing_agg_org_day ON briefing_aggregates(org_id, day);
