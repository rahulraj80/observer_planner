# CONTINUATION.md — Baseline Telemetry Implementation

> Read this first if a session is interrupted. It records where we are, what is pending, and the exact next action so work can resume without re-deriving context.

**Repository:** `observer_planner` (individual repo). Checkouts: `/mnt/scripts/observer_planner` on sup (azure-box, `testubuntu20`, where reporterd also runs) and `/home/ubuntu/scripts/observer_planner` on autobots.
**Branch:** `main`
**Plan home:** `docs/plans/baseline-telemetry-implementation-plan.md`
**Plan index:** `docs/plans/index.md`

## Status update — 2026-10-17 (sup session)

Rahul re-ordered the work:

1. **Sync from upstream first, then change.** Both forks merge their upstreams before any further telemetry or copy work:
   - `observer_planner` ← `superbasedapp/observer` (public; remote `upstream`, push disabled). Merged on branch `sync/upstream-2026-10-17`: v1.27.0 → the 2026-10-14 public snapshot (after v1.36.0). Follows upstream's repo split: the org server (`internal/orgserver`, `cmd/observer-org`, `web2`) is gone from this repo, and with it the fork's org-side Briefing (org rollup, org digest, `briefing_aggregates` / `virtuous_cycle` push). Porting the org Briefing to `superbased-observer-private` is a follow-up. The fork's migrations 078–085 collided with upstream's; they are now 164–170 (085 dropped: upstream 074 has the same index) and `internal/db/forklineage.go` repairs stores written by older fork builds.
   - `superbased-observer-private` ← `marmutapp/superbased-observer-private` (private). Upstream `main` is fetched by commit hash through the fork's own deploy key (`git fetch origin <sha>`), kept on local branch `upstream-main`. Next after the planner merge; Rahul ruled: keep and rewire the four org-enrichment features (salience extractor, manual re-fire, context-resend, bounded/thorough modes) onto upstream's evidence store; renumber the colliding agent (148/149) and org PG (0058–0061) migrations with the same lineage repair.
2. **Remove the "No telemetry" claims now** (was step 5.10, "last"): after both merges, replace every absolute no-telemetry / nothing-leaves claim across both repos and their published surfaces with truthful copy, kept in sync everywhere. Wording and publishing (website deploy, package listings) need Rahul's go-ahead.
3. Then resume at Step 5.2 below.

**Follow-ups opened by the 2026-10-17 planner merge:**
- Briefing / LLM-prompt / routing-benefit prose formats dollars directly (`internal/briefing/compose.go`, `justify.go`, `internal/llmrun/prompts.go`, `internal/routing/benefit.go`). They are allow-listed in `cmd/observer/no_bare_usd_test.go` as a known gap: route them through `internal/costtext` so the developer token display masks them.
- Port the org-side Briefing (org rollup, org digest, `briefing_aggregates` / `virtuous_cycle` push) into `superbased-observer-private`, where the org server now lives.
- The fork's nav pages (briefing, guide, modelfit, apiexplorer, licenses) and the LLM settings card are outside the org-governance vocabulary (adding them would bump `nodegov.CurrentVocabLevel`, a contract with the org server). Their API routes are governed through the Cost / Analysis / Settings>intelligence sections.

On sup, reads AND writes to `observer_reporter` work directly (the old "no sup write access" blocker below no longer applies when working on sup). GitHub access here is per-repo SSH deploy keys (`~/.ssh/config` aliases), not `gh`.

## Why this exists

We are implementing default-on baseline telemetry across both the individual repo (this one) and the enterprise repo (`../superbased-observer-private`), with the central server being `observer_reporter` on `sup` (`testubuntu20`, ingest `*:48089`, plaintext today). The objectives and locked decisions are in the master plan. This file is the interrupt-recovery record for the implementation work in **this repo**.

## Objectives (locked)

1. Remove "No telemetry" claims; replace with truthful non-advertising disclosure.
2. Default telemetry ON by default in both builds, same bounded content both builds: model, provider, input size, output size, cache percentage, end-to-end per-prompt time (full cycle, not TTFT), segregation user-prompt / system-nudge / tool-call. NO prompt text, NO output text.
3. Opt-in enrichment: prompts + session history for task identification / quality inputs, scoped to functionalities.
4. Giveaways / partner providers: in-app-launched sessions/terminals get free limited usage; track usage + quality inputs shareable with providers as thanks.

**Tier additivity rule (user ruling):** tiers are strictly ADDITIVE on top of the previous tier — no tier may subtract from or contradict the one below it. Baseline + enrichment = full enrichment (usage is just the baseline surface). Keep the additivity invariant in the contract's `TierCovers`/`TierAllows` semantics.

## Locked decisions (do not relitigate without explicit go)

- Individual registration: **no registration service.** Install key generated at install time; same key uploads data + authenticates; copyable from settings/profile; key alone is the credential for the web view; **individual self-view is API-only (1b)**.
- Enterprise: **both web dashboard and API.**
- Baseline goes to our central server in both builds; default endpoint `https://superbaseddev.bot.nu:48089`.
- Enterprise two solutions: self-hosted (to their reporterd) + cloud (to our server, into their tenant).
- Local-only rule for individual build: baseline only by default; prompt/output text, session history, enrichment, partner sharing, tenant features off-by-default or absent.
- System-nudge tagging: local, tag-only, improves over time; no prompt text transmitted.
- Commit-time drift prevention: contract + tier model + no-text-fields guarantee mirrored in both repos + reporterd, gated in CI.

## What has been done (so far)

- Master plan written: `docs/plans/baseline-telemetry-implementation-plan.md`
- Plan index written: `docs/plans/index.md`
- Current-state read performed on both repos and on reporterd (on sup):
  - Enterprise repo already has a full telemetry rail: `internal/telemetry/contract/contract.go` (textually identical to reporterd's contract.go by design), `golden/contract.json` v1.0.0 (10 events, tiers `off/error/usage/deep`, `off` default), `internal/config/telemetry.go` (default endpoint `http://127.0.0.1:48089`, tier `off` default, `Enabled=false` default, kill switch `OBSERVER_TELEMETRY`), `cmd/observer/telemetry.go` + `telemetry_loop.go` (show/consent/enable/disable/register/reset-id, disclosure text, server consent-ceiling loop), `internal/telemetry/derive.go` (deriveTokenUse emits `token_use` with `model_tier, role, tokens_in, tokens_out, cache_read`), `internal/telemetry/collector.go`.
  - Individual repo: **no `internal/telemetry/`, no `internal/config/telemetry.go`, no telemetry mentions anywhere.** Baseline rail must be built here from scratch.
  - Reporterd on sup: contract v1.0.0 + ingest (register needs user row; ingest dedupe + tier enforcement + quarantine + agg), auth via `Authorization: Bearer <install_token>` resolved into user/org/role, self/raw/summary + manager rollup + org summary exist, deletion matrix exists (`DELETE /v1/me`, `DELETE /v1/operator/user`, `DeleteInstall` per-install matrix in `internal/store/deletion.go`), org flow exists. Reporterd listens plain `*:48089`; no certbot, no reverse proxy on sup.

## Current step (completed — Step 5.1 verified)

**Step 5.1 — Shared contract + tier model + baseline event + no-text-fields test + CI drift gate.** COMPLETED and verified in this session.

### What landed
- reporterd contract v1.1.0: added `baseline` tier (on by default, with legacy `usage` normalized to it via `EffectiveTier`), `enrichment` placeholder tier, `EventPromptCycle` + closed `Model`/`Provider`/`Segment` vocabs, the no-text-fields `validateFields` case for `prompt_cycle`, `baselineEventSet`, `BaselineTierEvents()`, updated `TierAllows`/`TierCovers`/`EffectiveTier`, and the no-text-fields + prompt_cycle allowlist + cross-repo drift tests. Golden regenerated via `gen_contract.py`; `sample_batch.json.gz` now includes a baseline `prompt_cycle` event.
- reporterd committed here as `d656d07` (after the forbidden-trailer fix that replaced the initial `40d6805` commit which had contained a `Co-Authored-By` trailer). Pushed to `origin/main`. Sup pulled to a clean fast-forward checkout onto `origin/main`; sup-side `go test ./contract/...` + `make verify-telemetry-contract` both green against the cleaned v1.1.0 commit. Reporterd live on sup at `*:48089` (pid observed before push).
- enterprise repo mirror: `internal/telemetry/contract/` updated to v1.1.0 (contract.go textually identical to reporterd except this repo's header comment; `golden_gen.py` regenerated; `contract_test.go` extended with the new tests; full contract suite passes; `make verify-telemetry-contract` passes). Committed as `5171d224f` with author/committer `Rahul Raj <nomails@rahul.rocks>` and no forbidden attribution trailer. Not yet pushed (push when ready: `cd ~/scripts/superbased-observer-private && git push origin main`).
- individual repo mirror: `internal/telemetry/contract/` created (was absent before this step) — `contract.go`, `contract_test.go`, `golden_gen.py`, goldens, all mirroring reporterd; `go test ./internal/telemetry/contract/...` passes; `make verify-telemetry-contract` passes (`telemetry contract 1.1.0: golden artifacts match`). Committed as `3a0ede6` with author/committer `Rahul Raj <nomails@rahul.rocks>` and no forbidden attribution trailer. Not yet pushed (push when ready: `cd ~/scripts/observer_planner && git push origin main`).

### Correctness safeguards honored this session
- No `Co-Authored-By` / `Generated with` trailers anywhere in the contract commits (reporterd, enterprise, individual). The initial reporterd commit `40d6805` was replaced by `5f34a31` (later rebased/pushed as `d656d07` after the rehearsal marker) because it had contained a forbidden `Co-Authored-By` trailer.
- All three repos' contract version is `1.1.0`, with identical baseline event set + no-text-fields guarantee, so the drift gate fails if any one drifts.
- Sup-side contract verification used sup's own `go1.27.1` binary (force-local, `GOTOOLCHAIN=local`, `GOROOT=/usr/local/go`); the earlier attempt that used an aarch64 binary failed with "Exec format error" and was redone correctly.

## Blockers / things only the user can provide

- None for writing the Step 5.1 sub-plan file — I can proceed from the contract content already read in this session.
- For editing reporterd contract.go on sup: I need a working way to read + write files on sup from here. Earlier in this session, `ssh sup '... cat ...'` worked for reads; writes to sup are not yet confirmed working from this environment. **If sup file writes are not available from here, the reporterd contract delta must be done by you (or by logging into sup directly), and I should produce the exact delta as a patch/diff you can apply on sup.** This is the main operational blocker for the reporterd side of Step 5.1.

## Pending (not started)

- Step 5.2: Reporterd install-key model + individual API-only self view + delete/export by install key (needs sup write access or a patch for reporterd; also needs the install-key derivation detail locked).
- Step 5.3: Reporterd TLS on sup (needs certbot + a reverse proxy on sup; may need your go-ahead to install certbot/proxy on sup).
- Step 5.4: Reporterd self-host distribution story + runbook.
- Step 5.5: Reporterd enterprise tenant web dashboard + tenant API.
- Step 5.6: System-nudge classifier bootstrap + improvement loop (in the observer repos, local).
- Step 5.7: Enterprise repo baseline telemetry rail default-on (contract mirror + config default + emit baseline event + install key + settings exposure).
- Step 5.8: Individual repo baseline telemetry rail default-on (build rail from scratch here).
- Step 5.9: Giveaways / partner providers.
- Step 5.11: UI simplification — move less-used options into an Advanced/Export mode so the interface looks less daunting. (**User ruling: this is picked DEAD LAST, after all other development work is complete.**)
- Step 5.10: Remove "No telemetry" claims + rewrite privacy text (last).

## Install-key derivation detail (decision pending)

Not yet locked. Candidate: install key = 256-bit secret presented in copyable form in settings/profile; bearer token sent on the wire = sha256(key) (or similar); server stores sha256(key) the way it stores the install_token hash today; key is generated at install time in the observer app and exposed in settings/profile. This mirrors the existing reporterd pattern (token shown once, only hash stored) but makes the key itself the long-lived copyable credential rather than a one-time register response. Awaiting your confirm/correct before Step 5.2.

## sup access model (operational)

- Reads from sup worked via `ssh sup 'cat /mnt/scripts/observer_reporter/...'`.
- Writes to sup from here are not confirmed. If they are not available, produce reporterd deltas as patches/diffs for you to apply on sup, and continue the observer-repo side from here.
- TLS on sup (Step 5.3) and certbot/proxy install are actions on sup; flag as "your machine / your go-ahead" if they require credentials or actions I cannot perform.

## Commit discipline (for this work)

- This repo has no `check-commit-freshness.sh` and no CLAUDE.md of its own yet (none was found). The master plan's verification section is the source of truth for "done" per step.
- When code is edited in this repo, commit with a clear verification line naming what passed (typecheck, contract test, drift gate). Do not add bot attribution lines.
- Keep plan + code in the same commit when a plan step is implemented, so the doc moves with the code (the PSync CLAUDE.md Rule 19 discipline, adapted here).

## How to resume after interrupt

1. Read this file.
2. Read `docs/plans/baseline-telemetry-implementation-plan.md` for the full context.
3. Find the current step's "next action" above.
4. If the current step is 5.1 and the sub-plan file `docs/plans/contract-and-tier-model.md` exists, read it; otherwise write it from the 5.1 decisions/open-questions above.
5. If editing reporterd on sup: confirm sup write access; if unavailable, produce a patch/diff for the user to apply on sup and continue the observer-repo side from here.
6. After any edit, run the contract test + drift gate in all affected repos and record the result in the sub-plan's verification section before moving on.
