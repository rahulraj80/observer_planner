-- 077_cross_provider.sql - Cross-provider dispatch accounting
-- (docs/design/api-intelligence.md §5.7.7). Adds provider columns to
-- router_decisions and api_turns so cross-provider switches are fully
-- accounted and visible in evidence dashboards.
--
-- api_turns.provider stays the *serving* provider; original_provider
-- records the client's original intent (the provider dialect the client spoke).
-- translated flag marks when the request was shape-translated.

ALTER TABLE router_decisions ADD COLUMN original_provider TEXT NOT NULL DEFAULT '';
ALTER TABLE router_decisions ADD COLUMN selected_provider TEXT NOT NULL DEFAULT '';
ALTER TABLE router_decisions ADD COLUMN cross_provider    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE router_decisions ADD COLUMN translated        INTEGER NOT NULL DEFAULT 0;

ALTER TABLE api_turns ADD COLUMN original_provider TEXT;
ALTER TABLE api_turns ADD COLUMN translated        INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_router_decisions_providers ON router_decisions (original_provider, selected_provider);