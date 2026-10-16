# Step 5.1 — Shared contract + tier model + baseline event + no-text-fields test + CI drift gate

**Status:** implemented. Contract v1.1.0 landed in all three repos; baseline tier + prompt_cycle + no-text-fields + drift gate verified.
**Depends on:** none (this is the spine; everything else references it).
**Blocks:** Steps 5.2–5.10 (they all reference the contract + tier model + baseline event + drift gate).
**Read-first:** `docs/plans/baseline-telemetry-implementation-plan.md` (master plan), `docs/plans/CONTINUATION.md` (interrupt recovery).

## Purpose

Define the one shared contract that couples the three repos (reporterd on sup, enterprise observer repo, individual observer repo) for baseline telemetry. This step adds:

1. A **baseline tier** that is on by default in both builds.
2. A **baseline event** `prompt_cycle` with exactly the Point-2 fields and no prompt/output text fields.
3. A **no-text-fields enforcement + test** so the baseline event cannot accidentally admit prompt/output text.
4. A **CI drift gate** so the three repos' contracts cannot diverge.

This step does **not** change reporterd's existing `off/error/usage/deep` behavior for existing events. It adds a tier + an event + vocab + tests.

## Current contract state (grounding)

### Reporterd contract (on sup) — read earlier in this session
- `contract/contract.go` (textually identical to enterprise `internal/telemetry/contract/contract.go` by design; drift-checkable via `diff`).
- `golden/contract.json` v1.0.0:
  - `contract_version: "1.0.0"`, `envelope_version: "v1"`, `schema_version: "2026-09-14T00:00:00Z"`, `max_uncompressed_bytes: 1048576`, `max_argv_len: 120`.
  - events: `app_start`, `app_stop`, `session_start`, `session_end`, `tool_use`, `error`, `token_use`, `day_active`, `batch_meta`, `diag_command`.
  - tiers: `off` = [], `error` = [error, batch_meta], `usage` = [app_start, app_stop, session_start, session_end, tool_use, error, token_use, day_active, batch_meta], `deep` = usage + diag_command.
  - vocabularies: adapter (10), consent_tier (4), error_class (11), error_surface (7), model_tier (4), outcome (4), role (3), tool_name (16).
- Contract.go today:
  - Version = "1.0.0", EnvelopeVersion = "v1".
  - Envelope + Event types (Event has Event, EventVersion, SchemaVersion, TS, InstallID, Seq, SessionSpan, Classification, Purpose, Fields).
  - Tier constants: TierOff, TierError, TierUsage, TierDeep; ValidTiers; ValidTier; tierAllows; deepOnly; classificationPairs; validateFields (per-event allowlists).
  - Event names: EventAppStart, EventAppStop, EventSessionEnd, EventSessionStart, EventToolUse, EventError, EventTokenUse, EventDayActive, EventBatchMeta, EventDiagCommand.
  - Vocab maps: ToolName, ErrorClass, ErrorSurface, ModelTier, Role, Adapter.
  - Classification/purpose constants: ClassSystemMeta, ClassCallstack, ClassPseudonymized; PurposePerfHealth, PurposeFeature, PurposeBusiness.
  - validateFields per-event allowlists: tool_use (tool_name, count), error (error_class, surface), token_use (model_tier, role, tokens_in, tokens_out, cache_read), session_end (adapter, duration_ms, outcome), session_start (adapter), app_start/app_stop (version), diag_command (tool_name, argv_trunc, exit), batch_meta (dropped_count, retry_count), day_active (no fields).
  - DeepDisclosure + DeepDisclosureDigest for BR-10.7.

### Enterprise repo contract (local, `../superbased-observer-private`)
- `internal/telemetry/contract/contract.go` — textually identical to reporterd contract.go by design (drift-checkable via diff).
- `internal/telemetry/contract/golden/contract.json` — v1.0.0, same as reporterd golden (by design).
- Contract sync discipline already exists: contract.go is kept textually identical across reporterd + enterprise repo; golden artifacts byte-identical; pinned by `verify-telemetry-contract` Makefile target + TestGolden* tests. (Confirm whether the enterprise repo already has the Makefile target + TestGolden* tests; if so, mirror them into the individual repo.)

### Individual repo contract (this repo)
- **None.** `internal/telemetry/` does not exist. We create the mirror here in Step 5.1.

## Proposed delta

### Tier model
Add a **baseline** tier. Final tier set:

- `off` — nothing collected/staged/sent.
- `baseline` — the Point-2 set; **on by default in both builds.**
- `enrichment` — opt-in; may include prompts + session history for task identification / quality inputs, scoped to functionalities. Separate consent; never on by default. (Add the tier constant now; leave its event set placeholder until enrichment work. The no-text-fields test treats `enrichment` as "not baseline" so baseline stays clean.)
- `deep` — existing deep diagnostics (diag_command, digest-bound receipt, 90-day cap, purge-on-downgrade). Keep existing behavior.

T系統 constants to add/change:
- Add `TierBaseline = "baseline"`.
- Add `TierEnrichment = "enrichment"`.
- Update `ValidTiers` to: `off`, `baseline`, `enrichment`, `deep` (in escalation order).
- Update `ValidTier` switch to accept all four.
- Update `tierAllows`:
  - `TierOff` = {} (unchanged).
  - `TierBaseline` = the baseline event set (see below).
  - `TierEnrichment` = placeholder (empty or a future enrichment set); for now, treat enrichment as "not baseline" in the no-text-fields test.
  - `TierDeep` = nil (everything usage allows plus deep-only events) — but wait: if baseline is its own tier, does deep still imply usage, or baseline, or both? Plan intent: deep is the most-sensitive tier on top of the baseline; so deep should allow baseline events + diag_command. So `TierDeep` should allow baseline events + diag_command (deep-only). This means we need to define the baseline event set as the union of what deep allows minus diag_command, OR explicitly list baseline events in TierDeep too. Cleanest: define `baselineEvents` set, then `TierBaseline = baselineEvents`, `TierUsage` stays as today (usage events) OR we repurpose usage. **Decision point below.**
- Update `TierCovers` rank to include baseline + enrichment: `off=0, baseline=1, enrichment=2, deep=3`? Or `off=0, baseline=1, usage=2, enrichment=3, deep=4`? **Decision point below.**

#### Decision point A — what happens to the existing `usage` tier?
Today `usage` = the 9 existing usage events (app_start/stop, session_start/end, tool_use, error, token_use, day_active, batch_meta). Options:

- **Option A1 — keep `usage` as-is AND add `baseline` as a separate tier.** Then we have two "on by default-ish" tiers. This is confusing: which is the default? The plan says baseline is the default. So if we keep usage, we must say "baseline is the default; usage still exists for backward compatibility / for installs that were already on usage." That's workable but adds a tier most installs won't use.
- **Option A2 — rename/repurpose `usage` into `baseline`.** I.e., the on-by-default tier becomes `baseline`, and its event set is the Point-2 baseline set (which may or may not include all 9 existing usage events). Today's `usage` consumers (existing derive emits app_start/stop, session_start/end, tool_use, error, token_use, day_active, batch_meta) would need to be re-tiered: are those baseline events or still "usage"? The plan's baseline set is specifically the Point-2 fields — model, provider, input size, output size, cache %, end-to-end duration, segment — which is embodied in `prompt_cycle`. The other existing events (app_start/stop, session_start/end, tool_use, error, day_active, batch_meta) are broader app-lifecycle + tool counters + errors. Are those part of "baseline"? The plan's objective says baseline includes the Point-2 set; it does not explicitly say it includes app_start/stop etc. But it would be odd for "baseline" to be only `prompt_cycle` and nothing else. **Lean: baseline = `prompt_cycle` + the existing usage events that are also content-free and non-prompt/output (app_start/stop, session_start/end, tool_use, error, day_active, batch_meta).** So baseline is "the Point-2 prompt_cycle plus the existing content-free usage events." That keeps baseline broad enough to be a real default tier and keeps the no-prompt/output guarantee.
  - If we do that, then `usage` becomes redundant (baseline subsumes it). We could keep `usage` as a deprecated alias for baseline for one release, or remove it. **Lean: keep `usage` as an alias that maps to baseline for one release (EffectiveTier normalizes `usage` → `baseline`), then drop it later.** This avoids breaking any existing consumer that still says "usage."
- **Option A3 — repurpose `usage` to BE baseline (rename semantically, keep the constant `TierUsage` but change its meaning + event set to the baseline set).** This is the smallest diff but semantically muddies "usage" (which today means the 9 existing events). If we rename meaning, we should also rename the constant to avoid confusion. **Lean: prefer Option A2 (new `baseline` tier, keep `usage` as a deprecated alias) because it's the least ambiguous and matches the plan's "baseline on by default" language precisely.**

**Recommended:** Option A2. Add `baseline` tier with event set = `prompt_cycle` + the existing content-free usage events (app_start/stop, session_start/end, tool_use, error, day_active, batch_meta). Keep `usage` as a deprecated alias that normalizes to `baseline` for one release. Add `enrichment` tier constant now (placeholder event set), to be defined in the enrichment work. Deep allows baseline events + diag_command.

This means:
- `TierBaseline` event set = baseline events (prompt_cycle + the 9 existing usage events).
- `TierUsage` event set = same as baseline (deprecated alias), OR we keep `TierUsage` mapping to the same set and just add `TierBaseline` as the preferred name. Either way, both `baseline` and `usage` are admitted for the same events during the deprecation window.
- `TierDeep` event set = baseline events + diag_command (deep-only).
- `TierEnrichment` event set = placeholder (empty for now).

This keeps today's behavior intact (usage still admits the 9 events) while adding baseline as the preferred on-by-default tier.

#### Decision point B — tier rank / escalation
`TierCovers(have, want)` ranks tiers. Today: off=0, error=1, usage=2, deep=3. With baseline + enrichment added, rank options:
- `off=0, baseline=1, enrichment=2, deep=3` (if we treat enrichment as between baseline and deep).
- `off=0, baseline=1, usage=2, enrichment=3, deep=4` (if we keep usage as a distinct rank during deprecation).

**Recommended:** `off=0, baseline=1, usage=1 (alias, same rank as baseline), enrichment=2, deep=3`. I.e., baseline and usage are the same rank (aliases), enrichment is above baseline, deep is above enrichment. This keeps `TierCovers` semantics: baseline covers baseline; deep covers baseline + enrichment + diag_command; enrichment covers baseline + enrichment.

Actually, simpler: treat `usage` as exactly equal to `baseline` in rank (they are aliases). So rank map: off=0, baseline=1, usage=1, enrichment=2, deep=3. `TierCovers(have, want)` returns true if rank(have) >= rank(want). So a baseline-tier install covers baseline events; a deep-tier install covers baseline + enrichment + deep-only.

Wait — should deep cover enrichment? Today deep = usage + diag_command. If enrichment is a new opt-in tier for prompts+session-history, is deep "above" enrichment or separate from it? Plan: deep is deep diagnostics (diag_command, scrubbed). Enrichment is prompts+session-history for task identification/quality. These are different sensitivities. Which is "higher"? Probably enrichment is higher-sensitivity than deep (it has prompts), OR they are orthogonal. For tier-covers semantics, we need a total order. **Lean: enrichment is above baseline and below deep? No — prompts are more sensitive than diag_command. So enrichment should be ABOVE deep in sensitivity.** But deep has a 90-day cap and digest-bound receipt; enrichment would also need strong consent. Let's rank by sensitivity: off < baseline < deep < enrichment. I.e., enrichment is the highest-sensitivity tier (prompts + session history). Deep is above baseline (diag_command) but below enrichment. This makes `TierCovers` sensible: a deep-tier install does NOT cover enrichment events; an enrichment-tier install covers deep + baseline + enrichment.

Hmm, but today deep = usage + diag_command, and usage is being repurposed to baseline. So deep = baseline + diag_command. If enrichment is above deep, then enrichment = baseline + deep + enrichment events. That works: enrichment-tier allows everything deep allows plus enrichment events.

**Final recommended rank:** off=0, baseline=1, deep=2, enrichment=3. (Drop `usage` as a separate rank; it's an alias for baseline with rank 1.) `TierCovers(have, want)`: rank(have) >= rank(want). Deep covers baseline + deep-only (diag_command). Enrichment covers baseline + deep + enrichment events.

But wait — today's `tierAllows` has `TierDeep: nil` meaning "everything usage allows plus deep-only events." If we repurpose, deep should allow baseline events + diag_command. So `tierAllows[TierDeep]` = baseline events set + diag_command. And `deepOnly` = {diag_command}. And `TierEnrichment` allows baseline + deep + enrichment events (i.e., everything). For now, enrichment event set is placeholder (empty), so `TierEnrichment` effectively allows baseline + deep events for now, and we add enrichment events later.

OK this is getting deep. Let me settle the model concretely in the sub-plan file with the recommended defaults and mark the few remaining questions as "confirm with user." The key point for Step 5.1 is: add `baseline` tier (on by default), add `prompt_cycle` event with no-text-fields, add `enrichment` tier constant (placeholder), keep `deep` semantics, keep `off`, deprecate `usage` as alias for baseline. Rank: off < baseline < deep < enrichment.

### Baseline event `prompt_cycle`
- Event name constant: `EventPromptCycle = "prompt_cycle"`.
- classification: `ClassSystemMeta`.
- purpose: `PurposeBusiness`.
- event_version: 1.
- Fields (allowlist in validateFields):
  - `model` (string, closed vocab + `other` bucket).
  - `provider` (string, closed vocab + `other` bucket).
  - `input_size` (numeric, integer tokens).
  - `output_size` (numeric, integer tokens).
  - `cache_percentage` (numeric, 0–100).
  - `end_to_end_duration_ms` (numeric, integer ms, >= 0).
  - `segment` (string, closed vocab: `user_prompt`, `system_nudge`, `tool_call`).
  - correlation-only: `session_span` (string, optional), `turn_id` (string, optional — opaque id, not content), `install_id` is on the event already (enforced by validateEvent install binding).
- classificationPairs entry: `EventPromptCycle: {ClassSystemMetaData, PurposeBusiness}`.
- tierAllows: `TierBaseline` includes `EventPromptCycle`; `TierDeep` includes `EventPromptCycle` (deep allows baseline + diag_command); `TierEnrichment` includes `EventPromptCycle` (enrichment allows everything, for now).
- deepOnly: no change (diag_command stays deep-only). `prompt_cycle` is NOT deep-only; it's baseline.

### New closed vocabs
- `Model` vocab: start small, closed + `other`: anthropic, openai, google, cursor, copilot, codex, antigravity, cline, other. (Mirror the existing vocab discipline; grow over time.)
- `Provider` vocab: start small, closed + `other`: anthropic, openai, google, cursor, copilot, codex, antigravity, cline, other. (Provider vs adapter: adapter is the client/tool; provider is the model provider. They overlap but are distinct. Keep both; provider is what the plan asks for ("provider").)
- `Segment` vocab: `user_prompt`, `system_nudge`, `tool_call`. (No `other` bucket for segment? The plan wants segregation between user prompt / system nudges / tool calls. If a turn doesn't fit, what do we emit? Options: (a) require the classifier to always pick one of the three (force a best-guess), or (b) add `other` as a fallback. Lean: add `other` as a fallback so the classifier never has to omit a label, but treat `other` as "unclassified segment" and improve over time. So Segment vocab = user_prompt, system_nudge, tool_call, other.)

### validateFields case for EventPromptCycle
```
case EventPromptCycle:
    if !Model[get("model")] { return ErrBadSchema, "prompt_cycle.model" }
    if !Provider[get("provider")] { return ErrBadSchema, "prompt_cycle.provider" }
    if _, ok := num("input_size"); !ok || input_size < 0 { return ErrBadSchema, "prompt_cycle.input_size" }
    if _, ok := num("output_size"); !ok || output_size < 0 { return ErrBadSchema, "prompt_cycle.output_size" }
    if _, ok := num("cache_percentage"); !ok || cache_percentage < 0 || cache_percentage > 100 { return ErrBadSchema, "prompt_cycle.cache_percentage" }
    if _, ok := num("end_to_end_duration_ms"); !ok || end_to_end_duration_ms < 0 { return ErrBadSchema, "prompt_cycle.end_to_end_duration_ms" }
    if !Segment[get("segment")] { return ErrBadSchema, "prompt_cycle.segment" }
    // correlation-only optional fields:
    if v, ok := f["session_span"]; ok && v != "" { if _, ok := v.(string); !ok { return ErrBadSchema, "prompt_cycle.session_span" } }
    if v, ok := f["turn_id"]; ok && v != "" { if _, ok := v.(string); !ok { return ErrBadSchema, "prompt_cycle.turn_id" } }
    // ANY other field name = reject (no prompt/output text smuggling)
    for k := range f {
        switch k {
        case "model","provider","input_size","output_size","cache_percentage","end_to_end_duration_ms","segment","session_span","turn_id":
            // allowed
        default:
            return ErrBadSchema, fmt.Sprintf("prompt_cycle: unknown field %q", k)
        }
    }
```
This is the no-text-fields enforcement: any field name not in the allowlist is rejected. So even if a client tries to add `prompt_text` or `output_text` or `content`, it's rejected at the contract level.

### No-text-fields test (commit-time, all three repos)
Define a forbidden field-name set that represents "prompt/output text" concepts. The test asserts: for every event admitted by the baseline tier, the event's allowed field set (from validateFields / the contract) is disjoint from the forbidden set.

Forbidden field-name patterns (explicit, not fuzzy — list the names we want to forbid):
- Any field literally named: `prompt_text`, `prompt`, `output_text`, `output`, `content`, `text`, `body`, `message`, `message_body`, `request_text`, `response_text`, `input_text`, `output_text`, `raw_prompt`, `raw_output`, `prompt_body`, `response_body`.
- Plus a pattern check: any field name containing `prompt` or `output` or `content` or `text` or `body` or `message` — but only if it's not an allowed field. Actually, simpler and more robust: assert that the baseline tier's allowed field names (the union of all field names allowed by validateFields for baseline events) is disjoint from a curated forbidden set. The forbidden set is curated, not pattern-based, so it's exact and reviewable.

Curated forbidden set (baseline tier): `prompt_text`, `prompt`, `output_text`, `output`, `content`, `text`, `body`, `message`, `message_body`, `request_text`, `response_text`, `input_text`, `raw_prompt`, `raw_output`, `prompt_body`, `response_body`, `input_body`, `output_body`.

Test assertion: for each baseline event, collect the allowed field names (from the validateFields allowlist + correlation-only fields), assert none are in the forbidden set. Also assert `prompt_cycle`'s allowed set is exactly {model, provider, input_size, output_size, cache_percentage, end_to_end_duration_ms, segment, session_span, turn_id}.

This test lives in:
- reporterd: `contract/contract_test.go` (add a test, e.g. `TestBaselineNoPromptOrOutputText`).
- enterprise repo: `internal/telemetry/contract/contract_test.go` (mirror).
- individual repo: `internal/telemetry/contract/contract_test.go` (new, mirror).

### CI drift gate
A test (or Makefile target + test) that asserts the three repos' contracts agree on:
- contract version (all three same),
- tier set + ranks (all three same),
- baseline event set (all three same),
- baseline event allowed-field sets (all three same),
- no-text-fields assertion (all three same).

 Concretely: a test that loads each repo's contract golden (or contract.go constants) and asserts equality of the baseline event set + tier set + version. If they differ, the test fails in that repo. This is the drift gate.

 Implementation options:
 - If reporterd + enterprise already have a `verify-telemetry-contract` Makefile target + TestGolden* tests, extend them to also assert baseline event set + tier set equality across the three repos (or at least across reporterd + enterprise, and add the individual repo mirror + assertion).
 - If not, add a test `TestContractDrift` (or extend existing) that does the cross-repo assertion within each repo by loading the contract constants and comparing to a canonical expected value (the expected baseline event set + tier set + version). The "canonical expected" is the same in all three repos (copy the expected value, and if it drifts, all three fail until updated together).

 Lean: add a test in each repo that asserts its own contract's baseline tier + baseline event set + version matches a canonical expected value embedded in the test. Since the expected value is the same in all three repos (copy-paste), drift fails all three until they're updated together. Plus, keep the existing textual-identity discipline (diff contract.go across reporterd + enterprise) as a secondary check.

### Golden changes
- `golden/contract.json` (reporterd + enterprise + individual): bump `contract_version` to `1.1.0`; add `prompt_cycle` to events; add `baseline` + `enrichment` to consent_tier vocab; add `model`, `provider`, `segment` vocab arrays; add `EventPromptCycle` to the `baseline` tier's event list (and to `deep`'s list, since deep allows baseline + diag_command).
- Sample batches: if generated, regenerate to include a `prompt_cycle` sample event in the baseline tier. If hand-written, add a `prompt_cycle` sample event to the baseline sample batch. Confirm whether samples are generated or hand-written first.

### Contract version bump
- reporterd: Version constant `1.0.0` → `1.1.0`; golden `contract_version` → `1.1.0`.
- enterprise repo: mirror.
- individual repo: mirror (new file, version `1.1.0`).
- All three bump together. If one bumps and another doesn't, the drift gate fails.

## Files involved

### Reporterd (on sup)
- Edit: `/mnt/scripts/observer_reporter/contract/contract.go` (add TierBaseline, TierEnrichment, EventPromptCycle, Model/Provider/Segment vocab maps, classificationPairs entry, tierAllows entries, validateFields case, update ValidTiers/ValidTier/TierCovers, deprecate usage alias in EffectiveTier if we normalize usage→baseline).
- Edit: `/mnt/scripts/observer_reporter/contract/golden/contract.json` (bump version, add event + tiers + vocab).
- Edit: `/mnt/scripts/observer_reporter/contract/golden/sample_batch.json.gz` (regenerate or hand-edit to include prompt_cycle sample) — confirm generated vs hand-written first.
- Edit: `/mnt/scripts/observer_reporter/contract/contract_test.go` (add TestBaselineNoPromptOrOutputText + drift assertion).
- Possibly: `/mnt/scripts/observer_reporter/tools/gen_contract.py` / seed / demo scripts if samples are generated.

### Enterprise repo (local, `../superbased-observer-private`)
- Edit: `internal/telemetry/contract/contract.go` — textually identical to reporterd contract.go (mirror the delta).
- Edit: `internal/telemetry/contract/golden/contract.json` — mirror.
- Edit: `internal/telemetry/contract/golden/sample_batch.json.gz` — mirror.
- Edit: `internal/telemetry/contract/contract_test.go` — mirror the no-text-fields test + drift assertion (if it exists; if not, create it).
- Makefile: confirm `verify-telemetry-contract` target exists; if so, ensure it still passes after the delta; if not, consider adding a contract test target.

### Individual repo (this repo, `observer_planner`)
- Create: `internal/telemetry/contract/contract.go` — mirror of reporterd contract.go (the full file, textually identical by design).
- Create: `internal/telemetry/contract/golden/contract.json` — mirror.
- Create: `internal/telemetry/contract/golden/sample_batch.json.gz` — mirror (or omit samples if the individual repo doesn't need them yet; but the drift gate wants the golden, so include it).
- Create: `internal/telemetry/contract/contract_test.go` — mirror the no-text-fields test + drift assertion.
- Create: `internal/telemetry/contract/` + `doc.go` (package doc, mirroring reporterd's package doc).
- Possibly: a Makefile target or CI step that runs the contract test.

## Open questions to confirm before editing reporterd contract.go

1. **Supports `usage` as a deprecated alias for `baseline`?** Lean yes (normalize `usage` → `baseline` in EffectiveTier for one release, then drop). Confirm or correct.
2. **Tier rank order:** off < baseline < deep < enrichment? Or off < baseline < enrichment < deep? Lean: off < baseline < deep < enrichment (enrichment is most sensitive; prompts > diag_command). Confirm or correct.
3. **Deep allows baseline events?** Yes (deep = baseline + diag_command). Confirm.
4. **Enrichment event set placeholder now, defined later?** Yes (add the tier constant now; leave its event set empty/placeholder until enrichment work; no-text-fields test treats enrichment as "not baseline"). Confirm.
5. **Sample batches generated or hand-written?** Determine from `tools/gen_contract.py` / seed / demo scripts before editing samples.
6. **Does the enterprise repo already have `verify-telemetry-contract` Makefile target + TestGolden* tests?** Confirm before mirroring into the individual repo.
7. **Segment vocab: include `other` as fallback?** Lean yes. Confirm.
8. **Model/provider vocab: start closed + `other`, or allow normalized free-form strings with `other` bucket?** Lean: closed + `other`. Confirm. (We can expand the closed list over time; the point is no free-text prompt/output.)

## Verification (what "done" means — verified)

- reporterd `contract_test.go` passes, including `TestBaselineNoPromptOrOutputText` (baseline events admit no prompt/output text field names), `TestPromptCycleAllowlist` (prompt_cycle allowlist is the exact no-text set), `TestBaselineTierEvents` (baseline event set is the canonical 10), `TestContractDrift` (version + baseline tier + baseline event set + no-text-fields match the canonical expected shape), and the existing `TestTierAllows` / `TestTierCovers` / `TestDeepDisclosureDigestMatchesGolden` coverage.
- reporterd golden `contract.json` version is `1.1.0` and includes `prompt_cycle`, the `baseline` tier, the `enrichment` tier (placeholder event set), and `model` / `provider` / `segment` vocab arrays.
- reporterd `make verify-telemetry-contract` passes: regenerated goldens match the committed ones; the committed `sample_batch.json.gz` now includes a baseline `prompt_cycle` event.
- reporterd is live on sup at `*:48089`; sup re-verification passed: `go test ./contract/...` + `make verify-telemetry-contract` both green on sup against the cleaned v1.1.0 commit.
- enterprise repo (`superbased-observer-private`) contract.go is textually identical to reporterd contract.go except this repo's header comment; golden artifacts byte-identical; `go test ./internal/telemetry/contract/...` passes (full suite incl `TestVocabulariesMatchGolden`, `TestGoldenSampleBatchValid`, `TestGoldenSampleBatchInvalid`, `TestDeepDisclosureDigestMatchesGolden`); `make verify-telemetry-contract` passes (`telemetry contract v1.1.0: golden artifacts match`).
- individual repo (`observer_planner`) `internal/telemetry/contract/` created as a mirror: `contract.go` textually identical to reporterd except this repo's header comment, `contract_test.go` mirrors the reporterd tests, `golden_gen.py` mirrors `reporterd/tools/gen_contract.py`, goldens byte-identical with reporterd, `go test ./internal/telemetry/contract/...` passes (full suite incl `TestDeepDisclosureDigestMatchesGolden`), `make verify-telemetry-contract` passes (`telemetry contract 1.1.0: golden artifacts match`).
- CI drift gate: `TestContractDrift` embeds the canonical expected shape (v1.1.0, baseline tier, baseline event set, no-text-fields) in each repo's `contract_test.go`; `make verify-telemetry-contract` pins the regenerated goldens in each repo; together they fail if any one repo drifts on what "baseline" means.
- No prompt/output text field can be admitted by the baseline tier: enforced in `validateFields` for `prompt_cycle` (any field name not on the allowlist is rejected) and asserted by the no-text-fields tests in all three repos.

### Cross-repo ref hashes (at time of verification)

- reporterd here: `$(cd /home/ubuntu/scripts/observer_reporter && git rev-parse HEAD)`
- reporterd sup: `$(ssh -o ConnectTimeout=10 sup 'cd /mnt/scripts/observer_reporter && git rev-parse HEAD' 2>&1 | head -n 1)`
- enterprise repo: `$(cd /home/ubuntu/scripts/superbased-observer-private && git rev-parse HEAD)`
- individual repo: `$(cd /home/ubuntu/scripts/observer_planner && git rev-parse HEAD)`


## Out of scope for this step

- Actually emitting `prompt_cycle` from the collector/derive (that's Step 5.7/5.8).
- Install-key model, TLS, self-host, tenant dashboard, classifier, giveaways, privacy text rewrite (those are later steps).
- Changing existing `off/error/usage/deep` behavior for existing events (except the usage→baseline alias, which is backward-compatible).

## Next action after this sub-plan is finalized

1. Confirm/resolve the open questions above (especially #5 sample-batch generation, #6 enterprise Makefile target, #1 usage alias, #2 rank order).
2. Re-read the live reporterd contract.go + golden + contract_test.go from sup to confirm the current state matches what this sub-plan assumes (the content I read earlier may have changed, or I may have misread a detail).
3. Write the delta into reporterd contract.go + golden + contract_test.go (or produce a patch/diff for you to apply on sup if sup write access is unavailable).
4. Mirror into the enterprise repo.
5. Create the individual repo's mirror + test.
6. Run the contract test + drift gate in all three repos and record results in the verification section.

## Note on sup write access

If I cannot write files on sup from this environment, Step 5.1's reporterd side must be delivered as a patch/diff you apply on sup. In that case, I will:
- write the desired final state of reporterd `contract/contract.go`, `golden/contract.json`, `golden/sample_batch.json.gz` (if needed), and `contract_test.go` as separate files under `docs/plans/` (e.g. `docs/plans/reporterd-contract-delta/`), and
- provide a unified diff or per-file copy instructions so you can apply them on sup.

This keeps the work resumable and verifiable even if sup write access is unavailable from here.
