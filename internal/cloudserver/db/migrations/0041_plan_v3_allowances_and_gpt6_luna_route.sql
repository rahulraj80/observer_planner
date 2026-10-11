-- 0041_plan_v3_allowances_and_gpt6_luna_route.sql - plan v3 allowances and the
-- GPT-6 Luna enrichment route (operator ruling D-299, 2026-10-05).
--
-- WHAT MOVES
--
--   route  'session_enrichment.gpt-6-luna.v1' is a NEW route_registry row for
--          a GPT-6 Luna deployment (deployment name 'gpt-6-luna'). It is the
--          FEATURE DEFAULT candidate (plan_pinned = false) at route_version 2,
--          so the day it is activated it outranks 'session_enrichment.luna.v1'
--          (route_version 1) in pickDefaultRoute by version, never by a
--          route_id tie-break. It ships INACTIVE and UNBOUND (see below).
--   free v3       daily 20 -> 100, monthly 100 -> 3000. Concurrency 2, pool,
--                 digest, retention and label copied from v2. route_id is
--                 PINNED to the GPT-6 Luna route.
--   plus_beta v3  daily 25 -> 500, monthly 120 -> 15000 (5x free on both
--                 windows, per the ruling). Concurrency 4, pool, digest,
--                 retention and label copied from v2. route_id moves from the
--                 Sol route to the GPT-6 Luna route.
--
-- Operator ruling (verbatim): "increase the free limit ... to 100 a day and
-- 3000 a month - the signed-in version will have 5x more", clarified as
-- Signed-in Free 100/day + 3000/month and Plus 500/day + 15000/month; and
-- "we need to use gpt 6 luna for the enrichment work" (both plans).
--
-- No concurrency cap moves: the ruling names only the daily and monthly
-- windows, and Plus (4) already stays strictly above free (2), so the
-- "paid > free on every axis" rule 0039 documents still holds:
-- 500/15000/4 against 100/3000/2.
--
-- IMMUTABILITY. Plan rows are immutable (0012's sbci_plans_immutable BEFORE
-- UPDATE trigger), so a change is a NEW (name, version) row and the v1/v2 rows
-- stay byte-for-byte as they were. Like 0039, this migration only INSERTs into
-- plans, which the BEFORE UPDATE trigger does not fire on, so (unlike 0037's
-- in-place column backfill) it does NOT disable/enable the trigger. Every
-- column not deliberately moved is COPIED from the v2 row by INSERT ... SELECT,
-- and every insert is ON CONFLICT DO NOTHING, exactly 0039's idempotency.
--
-- EXISTING ROUTES ARE NOT MUTATED. 'session_enrichment.luna.v1' and
-- 'session_enrichment.sol.v1' are referenced by stored results
-- (analysis_results.model_route_id + their route/prompt/price snapshot) and
-- by the attestation cache keyed on (route_id, route generation). Rebinding
-- either of them to a different model would make those references lie, so
-- GPT-6 Luna is a NEW row with its OWN binding, attestation and kill switch.
-- The Sol row stays in the registry (still plan_pinned, still whatever an
-- operator left it), it is simply no longer named by any current plan
-- version: plus_beta v2 keeps pointing at it for history, and nothing
-- resolves v2 once its assignments are moved below.
--
-- WHY THE ROUTE SHIPS INACTIVE. Production enrichment is live on the bound
-- and attested Luna route. The GPT-6 Luna deployment does not exist yet on
-- any estate, so this row lands with active=false, no endpoint and no
-- api_version. With both v3 plans pinned to it, store.ResolveRouteForPlan
-- DEGRADES to the feature default (luna.v1) and emits the observable
-- plan_route_unavailable signal (reason 'inactive') - exactly 0039's Sol
-- posture. A route that was active but unbound would instead fail the
-- admission gate for every account (no credential / no attestation), i.e. a
-- fleet-wide 503; inactive is the posture that keeps service up. The 7-step
-- execution lease, the attestation gate and the kill switches are unchanged:
-- the route cannot serve a single job until an operator deploys it, binds it
-- (store.SetRouteBinding), it attests, and it is activated:
--   UPDATE route_registry SET active = true
--    WHERE route_id = 'session_enrichment.gpt-6-luna.v1';
-- After activation it is also the feature DEFAULT (route_version 2 > 1), so
-- the project-digest fallback (ResolveRouteForFeature) moves with it. The old
-- luna.v1 row can then be deactivated at the operator's leisure.
--
-- PRICE SNAPSHOT. 0.10 input / 0.50 output USD per MTok is the shipped
-- pricing table's `gpt-6-luna` row (internal/intelligence/cost/pricing.go and
-- pricing_snapshot.json, effective 2026-09-30; cache read 0.01, not stored on
-- route_registry). price_version stays 'unset' until an operator stamps a real
-- Azure cut, the same convention 0005 (Luna) and 0039 (Sol) follow.
-- max_output_tokens is 1024, Luna v1's value (operator ruling D-313, T-414:
-- cap the answer length and accept the residual risk). Reasoning tokens are
-- billed and counted as completion tokens on the chat_completions dialect, so
-- a long answer CAN be truncated at this cap; a truncated answer is incomplete
-- JSON, which the worker rejects as invalid output (retried within the attempt
-- budget, then failed with the user unit refunded), never stored as a partial
-- result. The cap bounds the worst-case output cost per call (see
-- docs/cloud-intelligence.md, "Worst-case model cost"). prompt_version starts
-- at 1 on this new route; route_version 2 already keeps its canonical job keys
-- disjoint from luna.v1's.
--
-- PRIVILEGE NOTE. Identical to 0039: plans / route_registry / kill_switches
-- are granted at TABLE level, and the account_plans DML below runs from the
-- migrator's connection exactly like 0039's subscriber move. No new grant.

-- ---------------------------------------------------------------------------
-- 1) The GPT-6 Luna route + its kill switch.
-- ---------------------------------------------------------------------------
INSERT INTO route_registry (
    route_id, feature, deployment, dialect, route_version, prompt_version,
    price_version, active, plan_pinned, max_output_tokens,
    input_price_per_mtok, output_price_per_mtok)
VALUES (
    'session_enrichment.gpt-6-luna.v1', 'session_enrichment', 'gpt-6-luna', 'chat_completions', 2, 1,
    'unset', false, false, 1024,
    0.10, 0.50)
ON CONFLICT (route_id) DO NOTHING;

-- Per-route kill switch, default OFF (0004's pattern; store.KillSwitches
-- treats a MISSING sentinel as ACTIVE/paused, so every route needs one).
INSERT INTO kill_switches (scope, key, active, generation)
    VALUES ('route', 'session_enrichment.gpt-6-luna.v1', false, 0)
    ON CONFLICT (scope, key) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 2) The v3 plan rows.
-- ---------------------------------------------------------------------------
INSERT INTO plans (name, version, label, daily_cap, monthly_cap, concurrency_cap,
                   budget_pool, digest_weekly, results_retention_days, route_id)
SELECT 'free', 3, p.label, 100, 3000, p.concurrency_cap,
       p.budget_pool, p.digest_weekly, p.results_retention_days, 'session_enrichment.gpt-6-luna.v1'
  FROM plans p
 WHERE p.name = 'free' AND p.version = 2
ON CONFLICT (name, version) DO NOTHING;

INSERT INTO plans (name, version, label, daily_cap, monthly_cap, concurrency_cap,
                   budget_pool, digest_weekly, results_retention_days, route_id)
SELECT 'plus_beta', 3, p.label, 500, 15000, p.concurrency_cap,
       p.budget_pool, p.digest_weekly, p.results_retention_days, 'session_enrichment.gpt-6-luna.v1'
  FROM plans p
 WHERE p.name = 'plus_beta' AND p.version = 2
ON CONFLICT (name, version) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 3) Move every assignment on an OLDER free / plus_beta version to v3.
-- ---------------------------------------------------------------------------
--
-- Unassigned accounts need nothing: they resolve store.DefaultFreePlanVersion,
-- which this change bumps to 3 in the same commit. Accounts WITH a row are
-- pinned to an immutable older plan_id and would keep the old allowance (and,
-- for Plus, the Sol pin) until someone re-assigned them, so they move here, in
-- the transaction that publishes v3. "Older" is version < 3, not just v2: an
-- explicit free v1 row (a pre-0039 cancellation) or a plus_beta v1 row (a
-- purchase on a price still mapped to v1) is the same stale-allowance problem.
--
-- (a) Rows IN FORCE that started strictly before now(): 0039's exact shape -
--     close at now(), open the v3 row at now(), PRESERVING the closed row's
--     own effective_until and source, so a pending scheduled change after it
--     stays legal against account_plans_no_overlap.
CREATE TEMP TABLE sbci_0041_moves ON COMMIT DROP AS
SELECT ap.id, ap.account_id, ap.source, ap.effective_until AS old_until, p.name AS plan_name
  FROM account_plans ap
  JOIN plans p ON p.plan_id = ap.plan_id
 WHERE p.name IN ('free', 'plus_beta')
   AND p.version < 3
   AND ap.effective_from < now()
   AND (ap.effective_until IS NULL OR ap.effective_until > now());

UPDATE account_plans
   SET effective_until = now()
 WHERE id IN (SELECT id FROM sbci_0041_moves);

INSERT INTO account_plans (account_id, plan_id, effective_from, effective_until, source)
SELECT m.account_id, v3.plan_id, now(), m.old_until, m.source
  FROM sbci_0041_moves m
  JOIN plans v3 ON v3.name = m.plan_name AND v3.version = 3;

-- (b) Rows that have NOT STARTED: a scheduled future assignment (a graceful
--     cancellation's free downgrade at the cycle boundary, say) or a row that
--     starts at exactly now(). The second shape is real: the runner applies
--     every pending migration in ONE transaction, so on an estate jumping
--     38 -> 41 the v2 rows 0039 just opened carry effective_from = now(),
--     which (a)'s strict "< now()" cannot close without violating 0012's
--     CHECK (effective_until > effective_from). No allowance has been served
--     under a row that has not started, so its plan_id is repointed IN PLACE
--     to the v3 row of the same name - no history is rewritten, and the
--     window (effective_from / effective_until) is untouched, so no overlap
--     can appear. Without this a cancelling Plus subscriber would land on
--     free v2 (20/100) instead of free v3 at the boundary.
UPDATE account_plans ap
   SET plan_id = v3.plan_id
  FROM plans old, plans v3
 WHERE old.plan_id = ap.plan_id
   AND old.name IN ('free', 'plus_beta')
   AND old.version < 3
   AND v3.name = old.name
   AND v3.version = 3
   AND ap.effective_from >= now();
