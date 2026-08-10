-- 075_license_usage.sql - License/plan inventory telemetry
-- (docs/design/api-intelligence.md §5.2). Records observed usage per
-- license/plan so the dashboard can show utilization, break-even, and
-- make recommendations. Entitlement kind (subscription/api/plan) is
-- detected from the request auth; license_id is resolved from the
-- [licenses] config when available, else a synthetic unknown-<provider>-<kind> key.

CREATE TABLE IF NOT EXISTS license_usage (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    license_id             TEXT    NOT NULL,           -- from [licenses] config or synthetic
    observed_at            TEXT    NOT NULL,           -- RFC3339 UTC bucket start
    entitlement_kind       TEXT    NOT NULL,           -- subscription | api | plan
    provider               TEXT    NOT NULL,
    models_used_json       TEXT    NOT NULL,           -- JSON array of model names
    turns                  INTEGER NOT NULL DEFAULT 0,
    input_tokens           INTEGER NOT NULL DEFAULT 0,
    output_tokens          INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens      INTEGER NOT NULL DEFAULT 0,
    cache_creation_tokens  INTEGER NOT NULL DEFAULT 0,
    cost_usd_list          REAL    NOT NULL DEFAULT 0, -- list-basis cost for the bucket
    quota_monthly_tokens   INTEGER                    -- from config when known (NULL = unmetered/unknown)
);

CREATE INDEX IF NOT EXISTS idx_license_usage_license_ts ON license_usage (license_id, observed_at);
CREATE INDEX IF NOT EXISTS idx_license_usage_provider_ts ON license_usage (provider, observed_at);