-- 076_dispatch_benefit.sql - Expected-benefit record for routing decisions
-- (docs/design/api-intelligence.md §5.4). Extends router_decisions with
-- a structured expected-benefit breakdown + human text, plus the
-- entitlement/effective basis used, so dashboards can show "what the
-- router thought it would save vs what actually happened".

ALTER TABLE router_decisions ADD COLUMN benefit_json  TEXT;
ALTER TABLE router_decisions ADD COLUMN benefit_text  TEXT;
ALTER TABLE router_decisions ADD COLUMN entitlement_kind TEXT;
ALTER TABLE router_decisions ADD COLUMN effective_basis    TEXT;
ALTER TABLE router_decisions ADD COLUMN realized_delta_usd REAL;