-- 078_briefing_snapshots.sql - Daily Briefing snapshot (docs/design/
-- standup-briefing.md §2.5, Phase A). Stores the last COMPOSED briefing
-- per local day for reproducibility + audit ("show me yesterday's briefing
-- again, as generated"). Written by the composer; read by the page and the
-- daily email.
--
-- NODE-LOCAL: the snapshot is a presentation artifact of the node's own
-- briefing. It is NOT read by internal/store/orgpush.go::SelectUnpushedSince
-- (that seam enumerates only the capture tables), and it must never be added
-- there (privacy sentinel: tests/invariant/privacy_test.go). No paired
-- orgserver migration, by design.

CREATE TABLE IF NOT EXISTS briefing_snapshots (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    date         TEXT NOT NULL UNIQUE,   -- local day "YYYY-MM-DD" the briefing describes
    json         TEXT NOT NULL,          -- full Briefing JSON as composed
    generated_at TEXT NOT NULL           -- RFC3339 UTC compose time
);
