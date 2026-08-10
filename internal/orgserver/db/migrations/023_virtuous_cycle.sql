-- Virtuous-Cycle trend rollup (docs/design/standup-briefing.md, Stage 3).
-- Content-free cumulative CTA applies with projected vs realized savings,
-- pushed by nodes that opted into the node-side [org_client.share] virtuous_cycle
-- toggle. Every key is content-free: week_start (Monday UTC), tool (adapter name);
-- no CTA ledger content or raw router_decisions rows cross the wire, and the org
-- admin cannot force more. The server upserts by the (org_id, user_email, week_start)
-- natural key — windowed-recompute at the node means a re-pushed window is
-- exactly idempotent, so the same bucket may be overwritten as the node's
-- local view of a completed week settles.
CREATE TABLE IF NOT EXISTS virtuous_cycle (
    org_id        TEXT NOT NULL,
    user_email    TEXT NOT NULL,
    week_start    TEXT NOT NULL, -- UTC YYYY-MM-DD (Monday)

    applies       INTEGER NOT NULL DEFAULT 0,
    projected_usd REAL    NOT NULL DEFAULT 0,
    realized_usd  REAL    NOT NULL DEFAULT 0,
    realized_turns INTEGER NOT NULL DEFAULT 0,

    updated_at    TEXT NOT NULL,
    pushed_by_user_id TEXT NOT NULL, -- authenticated pusher, never the wire value

    PRIMARY KEY (org_id, user_email, week_start)
);
CREATE INDEX IF NOT EXISTS idx_virtuous_cycle_org_week ON virtuous_cycle(org_id, week_start);