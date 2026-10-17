# Baseline Telemetry Implementation Plan

**Status:** Approved for implementation (2026-10-10).
**Scope:** Both repos — enterprise (`superbased-observer-private`) and individual (`observer_planner`).
**Central server:** `observer_reporter` on `sup` (`testubuntu20`), ingest `*:48089`, plain today.
**Default endpoint (both builds):** `https://superbaseddev.bot.nu:48089` (TLS-terminate on `sup`; reporterd has no TLS today).

---

## 1. Objectives (from user) — the non-negotiables

1. **Remove the "No telemetry" claims** from README/PRIVACY and all surfaces in both repos. Replace with truthful, non-advertising disclosure that matches what actually ships.
2. **Default telemetry ON by default in both builds.** Same bounded content in both:
   - model used
   - provider
   - context length / input size
   - output size
   - caching percentage
   - end-to-end time per prompt-response cycle (full prompt→response, NOT TTFT)
   - segregation between **user prompt / system nudges / tool calls**
   - **NO prompt text, NO output text** — not in default telemetry.
3. **Opt-in enhancement/enrichment:** user consents to send prompts + session history to improve task identification and quality inputs, scoped to the app's functionalities.
4. **Giveaways / partner API providers:** launch giveaways; free limited usage for users who launch coding sessions/terminals from inside the app; track usage + quality inputs that can be shared with API providers as thanks for token gifts.

## 2. Decisions (locked 2026-10-10)

1. **Individual registration: no registration service.** Generate an installation key at install time; same key uploads data + authenticates the user; copyable from settings/profile; key alone is the credential for the web view; **individual self-view is API-only (1b)**.
2. **Enterprise: both web dashboard and API.**
3. **Baseline goes to our central server in both builds**, default endpoint `https://superbaseddev.bot.nu:48089`.
4. **Enterprise two solutions:** (1) self-hosted — prompt telemetry to their reporterd; (2) cloud — to our server, into their tenant, web dashboard + API.
5. **Local-only rule (suggested, approved in principle):** individual build ships only the baseline to the central server by default; prompt/output text, session history, enrichment, partner sharing, tenant features are off-by-default or absent. Enterprise build adds self-hosted/cloud-tenant + optional consented enrichment + optional consented giveaways.
6. **System nudge tagging (g):** classify locally from DB text via pareto/frequency bootstrap + continuously improving classifier; emit only the tag; never export prompt text.
7. **Commit-time drift prevention (e):** contract + tier model + no-text-fields guarantee mirrored in both repos + reporterd and gated in CI.

## 3. Current state (grounding for implementation)

### 3.1 Enterprise repo (private)
- Has the telemetry rail: `internal/telemetry/contract/contract.go` (textually identical to `observer_reporter/contract/contract.go`), `golden/contract.json` v1.0.0 (10 events, tiers `off/error/usage/deep`, `off` default), `internal/config/telemetry.go` (default endpoint `http://127.0.0.1:48089`, tier `off` default, `Enabled=false` default, kill switch `OBSERVER_TELEMETRY`), `cmd/observer/telemetry.go` + `telemetry_loop.go` (show/consent/enable/disable/register/reset-id, disclosure text, server consent-ceiling loop), `internal/telemetry/derive.go` (deriveTokenUse emits `token_use` with `model_tier, role, tokens_in, tokens_out, cache_read`), `internal/telemetry/collector.go`.
- Install-id minting exists (`randomUUID()`, `InstallID()` in collector), but **no install-key concept that doubles as the personal-view credential**; the enterprise rail uses `install_token_file` from `POST /v1/register` (which requires a `users` row).

### 3.2 Individual repo (current, `observer_planner`)
- **No `internal/telemetry/`, no `internal/config/telemetry.go`, no telemetry mentions anywhere.** Confirmed by read. Build the baseline rail from scratch here.

### 3.3 Reporterd server (on sup, `observer_reporter`)
- Contract v1.0.0 (`contract/contract.go` + `golden/contract.json` + sample batches). Envelope + 10 events + closed vocabs + classification×purpose matrix + tierAllows. Baseline today = tierUsage (app_start/stop, session_start/end, tool_use, error, token_use, day_active, batch_meta). `off` default today.
- Ingest: `POST /v1/register` requires a `user_id` in the users population, mints `install_id` + `install_token`, stores token hash. `POST /v1/ingest` gzip JSON, dedupe on batch_uuid, enforces consent tier + org policy floor + deep-receipt liveness, quarantines bad batches, builds agg_user_day.
- Auth: `Authorization: Bearer <install_token>` resolved via `store.ResolveToken` into user/org/role identity. So the install_token is close to what we want for individuals, but it's minted by register and tied to a user row.
- Visibility planes exist: self raw (`GET /v1/me/raw`, unsealed-for-them), self summary (`GET /v1/me/summary`), manager rollup with k-suppression, peer grants, operator unseal+raw, org summary.
- Org flow exists: dual-consent join, sever, backfill.
- Deletion matrix exists: `DELETE /v1/me` (user deletes all installs + user row), `DELETE /v1/operator/user`. Per-install matrix in `internal/store/deletion.go` (`DeleteInstall`) already does the per-install deletion.
- TLS: reporterd listens plain `*:48089`; no certbot, no reverse proxy on sup.
- Self-host story: repo is self-contained (Go + Postgres + KEK + seed + tools/gen_seed.py + tools/gen_contract.py + seed/ + demo scripts). Distribution doc/runbook not yet explicit.

## 4. Build order (priority)

1. **Shared contract + tier model + baseline event + no-text-fields test + CI drift gate (docs/plans/contract-and-tier-model.md).**
2. **Reporterd server: install-key model + individual API-only self view + delete/export by install key (docs/plans/reporterd-install-key-and-self-view.md).**
3. **Reporterd: TLS on sup (docs/plans/reporterd-tls.md).**
4. **Reporterd: self-host distribution story (docs/plans/reporterd-self-host.md).**
5. **Reporterd: enterprise tenant web dashboard + tenant API (docs/plans/reporterd-tenant-dashboard.md).**
6. **Reporterd: system-nudge classifier bootstrap + improvement loop (docs/plans/system-nudge-classifier.md).**
7. **Enterprise repo: baseline telemetry rail default-on (docs/plans/enterprise-baseline-rail.md).**
8. **Individual repo: baseline telemetry rail default-on (docs/plans/individual-baseline-rail.md).**
9. **Giveaways / partner providers (docs/plans/giveaways-partner-providers.md).**
10. **Remove "No telemetry" claims + rewrite privacy text (docs/plans/privacy-disclosure-rewrite.md).**

## 5. Combined detailed plan

### 5.1 Shared contract + tier model + baseline event + no-text-fields test + CI drift gate

#### Tier model (one model everywhere)
- `off` — nothing collected/staged/sent.
- `baseline` — the Point-2 set, **on by default in both builds**. New default tier in both repos and in reporterd for new individual installs.
- `enrichment` — opt-in; may include prompts + session history for task identification / quality inputs, scoped to functionalities. Separate consent, never on by default.
- `deep` — keep existing deep diagnostics (diag_command, digest-bound receipt, 90-day cap, purge-on-downgrade). Existing behavior preserved.

Kill switch: keep `[telemetry] enabled=false` + env `OBSERVER_TELEMETRY=off` hard-stopping emit + network in both observer repos. In reporterd, keep refusing inbound baseline for an install whose effective tier is off (today's `tier == off → consent_missing` already does this; keep it and make `baseline` the default for new individual installs).

Default tier for new installs:
- Individual install (no user/org): `baseline` on by default.
- Enterprise install: `baseline` on by default at install time; org policy floor can lower it (extend org policy so a tenant can set the tenant's default tier, but baseline remains the individual default unless the org sets otherwise).

#### Baseline event (Point-2 fields, no prompt/output text)
Add a new event to the contract, `prompt_cycle` (new event, not a mutation of existing `token_use`, because the Point-2 set is a per-prompt-cycle signal distinct in shape from the existing per-(session,model,tier,role) aggregate `token_use`):

- classification: `SystemMetaData`
- purpose: `BusinessInsight`
- fields (all closed or numeric; no free text):
  - `model` — string, closed vocab + `other` bucket (model identifier, not prompt text).
  - `provider` — string, closed vocab (anthropic, openai, google, cursor, copilot, codex, antigravity, other, etc.).
  - `input_size` — integer tokens.
  - `output_size` — integer tokens.
  - `cache_percentage` — numeric 0–100.
  - `end_to_end_duration_ms` — integer ms, full prompt→response cycle, not TTFT.
  - `segment` — closed vocab: `user_prompt`, `system_nudge`, `tool_call`.
  - correlation fields only: `session_span`, `turn_id` (opaque id, not content), `install_id`, timestamps.

Add to contract.go:
- `EventPromptCycle = "prompt_cycle"` constant.
- `classificationPairs[EventPromptCycle] = {ClassSystemMetaData, PurposeBusiness}`.
- `tierAllows[TierBaseline]` (or fold baseline into the tier that is on by default — see tier model; if baseline is its own tier, add `TierBaseline` constant + entry in ValidTiers + tierAllows).
- `validateFields` case for `EventPromptCycle`: allowlist exactly the Point-2 field names above; reject any other field name. This is the no-prompt-text/no-output-text enforcement in the contract.
- Add vocab maps as needed: `Model` (closed model vocab + other), `Provider` (closed provider vocab + other), `Segment` (user_prompt / system_nudge / tool_call).

Add to golden/contract.json:
- `prompt_cycle` in `events` with its classification/purpose.
- `baseline` tier in `tiers` (if baseline is its own tier) listing `prompt_cycle` plus the existing usage events it should also carry, or list only `prompt_cycle` if baseline is precisely the Point-2 set.
- new vocab arrays for `model`, `provider`, `segment` if they are new closed vocabs.

#### No-text-fields guarantee (enforcement + test)
- Contract-side: `validateFields` for `prompt_cycle` allows only the named fields, so a client cannot smuggle a prompt/output text field by name.
- Test (commit-time, in reporterd + both observer repos): enumerate the baseline tier's allowed events and assert none admit a prompt-text or output-text field name. Define the forbidden field-name set explicitly (e.g. any field whose name suggests prompt/output/content/text body) and assert the baseline event's allowed field set is disjoint from it. This is the drift gate (e).
- Client-side: envelope builder must not include prompt/output text in baseline event fields; the contract test is the backstop.

#### Segregation vocab + system-nudge definition (g ties in)
- Freeze `segment` vocab: `user_prompt`, `system_nudge`, `tool_call`.
- Operational definition of `system_nudge`: anything the app/agent emitted as a nudge, suggestion, system prompt, auto-composed guidance, automatic next-action hint, or similar non-user-authored, non-tool-call turn component.
- Classification is local and imperfect at first; improves over time; only the tag is emitted; prompt/output text never leaves.

#### Contract mirror + CI drift gate
- Reporterd `contract/` is the versioned source of truth for the server side (already true).
- Both observer repos mirror the same contract (event names, tiers, vocabs, classification×purpose, field allowlists). Enterprise repo already has `internal/telemetry/contract/`; individual repo gets one.
- CI gate: a test that (1) loads the contract golden from each repo, (2) asserts the baseline tier's event set matches reporterd's baseline event set, (3) asserts none of those events admit prompt-text/output-text field names. Run in all three repos. If contract version bumps, all three must bump together (mirror rule already exists in reporterd AGENTS.md; extend it to the baseline event + tier model).

#### Versioning
- Baseline event addition = minor contract bump (additions are minor per the contract's own rule).
- If we instead extended `token_use` meaning, that's a meaning change and needs a new event name + major bump. Recommend the new-event path to avoid mutating existing `token_use` consumers.

### 5.2 Reporterd server: install-key model + individual API-only self view + delete/export by install key

#### Install-key model (replaces user-row-dependent register for individuals)
- **Individual install key:** generated at install time by the client (observer repo), not by reporterd register. High-entropy secret (256-bit random, presented in a copyable form). The client derives from it:
  - a public `install_id` (`inst-<12 hex>` shape, same as today — random, not hardware-derived, resettable),
  - an auth credential used for `Authorization: Bearer <key-derived-token>` on ingest and on the personal API view.
- Reporterd accepts the install key (or a key-derived bearer token) and resolves it to an install **without requiring a `users` row**. So individuals have an install + key but no user row. The `users` table stays for enterprise tenants and any individual the operator wants to account for, but it is not required for an individual install to exist or use baseline telemetry.
- Enterprise tenants: unchanged — users/orgs/roles/dashboard still work; an enterprise install can also be bound to a user/org, and the tenant dashboard authenticates via the enterprise identity model.

This keeps "no registration needed" for individuals while preserving enterprise user/org semantics. The install key is the individual's credential for everything: upload + personal API view + delete/export.

#### Ingest auth that accepts install key without user row
- Extend token resolution so a bearer token can resolve to either (a) an enterprise user/org identity, or (b) an individual install-only identity (install_id, no user_id, no org_id, role = individual/self).
- Ingest admission still checks consent tier (baseline on by default → admitted; off → consent_missing).
- The install-only identity can read/write its own rows via RLS (install-scoped), but has no org, no manager rollup, no cross-install visibility.
- Enterprise tenant tokens still resolve to user/org/role and get the org dashboard + manager rollup.

#### Individual API-only self view (1b)
Individual uses their install key to call:
- `GET /v1/me/summary` — their own aggregates (install-scoped).
- `GET /v1/me/raw` — their own raw events, unsealed for them.
- `GET /v1/consent/config` — their own consent posture + server floors.
- `GET /v1/consent/deep` etc. as applicable.

No web login for individuals. The install key is pasted into whatever client they use to view their data, or used by the observer app itself to display the user's own analytics. This is (1b).

#### Install-key copyable from settings/profile
In the observer app (both repos), the install key is:
- generated at first install/first-run,
- stored locally (scrubbed from logs, file permissions appropriate),
- shown in settings/profile as a copyable key,
- used by the app to authenticate baseline upload + to let the user paste it into a viewer if they want their own API view.

Provide a "reset/install-new-key" path that rotates the key and orphans prior history (documented trade-off, same as today's reset-id trade-off). Resetting the key invalidates the old key server-side (revoke that install's token / mark stale), which is also the individual's "stop sending from this install" control.

#### Delete/export by install key (geo regimes)
- Reporterd already has a deletion matrix for user-based deletion. Add **install-key-scoped deletion**: `DELETE /v1/me` already works for the caller's own identity; for individuals that's install-scoped deletion of raw_events, batches, receipts, agg, install row. Ensure it works for install-only identity (not just user-bound identity).
- Add an **export** endpoint for the individual's own data (their raw events + aggregates for that install) so a user in a geo regime can export before deleting. Export is authenticated with the install key, scoped to that install.

Implementation: extend `handleSelfDelete` (or add a parallel handler) so the install-only identity can delete its own install + export its own data. The existing deletion matrix in `internal/store/deletion.go` (`DeleteInstall`) already does the per-install matrix; wire an API path that calls it for the install-only identity and returns honest counts. Add `GET /v1/me/export` (or similar) that returns the install's own data (raw + agg) for download, authenticated by install key.

### 5.3 Reporterd: TLS on sup

- Install certbot on sup, get a cert for `superbaseddev.bot.nu`, terminate TLS in front of reporterd with a minimal reverse proxy (caddy is the smallest path: single binary, automatic LE certs; or nginx + certbot). Reporterd stays on a local port (keep 48089 internal, or move it to a localhost port and expose 48089 as the TLS-terminated proxy).
- Verify `https://superbaseddev.bot.nu:48089` serves a trusted cert from sup and from outside.
- If reporterd has TLS support internally (confirm from main.go in implementation), reporterd-native TLS is an option; but given no certbot/proxy today and reporterd's current plaintext listen, proxy-termination is the safer, faster path and matches "enable certbot if not already there."

### 5.4 Reporterd: self-host distribution story

- Reporterd is already self-contained: `cmd/reporterd/main.go`, `internal/store/` + migrations, `internal/store/sealbox.go` (sealed-raw with KEK, `REPORTERD_KEK`), `tools/gen_seed.py`, `tools/gen_contract.py`, `seed/`, demo scripts.
- Self-host story = "run reporterd with your own Postgres, your own KEK (generated via gen_seed.py or equivalent), your own seed (optional), pointed at by your own TLS endpoint."
- If the current code already supports running with a customer's own Postgres/KEK/seed, we mostly need to **document + verify** it (a runbook + a clean first-run path).
- If something is missing (e.g., no clean KEK generation for customers, no doc, no demo that mimics a customer deploy), add the missing piece and produce the runbook.
- Assess in implementation from `cmd/reporterd/main.go` + store init + seed tools.

### 5.5 Reporterd: enterprise tenant web dashboard + tenant API

- Build a thin tenant dashboard in reporterd (or as a small reporterd-owned service) where a tenant admin logs in and sees:
  - tenant identity + consent posture,
  - tenant aggregate view (reuses `GET /v1/org/summary` / manager rollup shape),
  - the tenant's installs,
  - a path to export/delete tenant data (within their rights),
  - org join/sever posture where applicable.
- Tenant login uses the enterprise identity model (users/orgs/roles). The dashboard is RLS-scoped so a tenant only sees its own data.
- This is the "web" half of "both web and API"; the API half already exists (`/v1/org/summary`, manager rollup, `/v1/me/*`, delete, join/sever). The dashboard is primarily a view layer over those.
- If `handleDashboard` in reporterd is already a dashboard skeleton, extend it; if not, add a minimal one. Keep it tenant-isolated and simple.

### 5.6 Reporterd: system-nudge classifier bootstrap + improvement loop

- Classification happens in the observer client (where the DB text lives), not in reporterd.
- Write an analysis tool/job in the observer repo that scans the existing local DB text (prompts, nudges, tool calls, system messages as available), computes frequency/pareto of candidate system-nudge patterns, and produces an initial system-nudge signature/ruleset. This bootstrap is local, offline, and does not send prompt text anywhere. It outputs the classification rules/signature used by the segment classifier.
- Local segment classifier in the observer repo tags each turn/segment as `user_prompt`, `system_nudge`, or `tool_call` using the bootstrap signature + heuristics. Only the tag is emitted in the baseline `prompt_cycle` event. Prompt/output text never sent to reporterd.
- Continuous improvement loop: classifier can improve over time using aggregates/telemetry data (e.g., which tags are frequently ambiguous, which patterns correlate with user corrections if the app ever collects such signals locally). Improvement is local and tag-only. Reporterd never receives prompt text for this; it only receives the segment tag in the baseline event.
- Document what "system nudge" means operationally, and note that classification is local and imperfect at first, improving over time, and that no prompt/output text is transmitted for classification. Belongs in the privacy text later and in the classifier docs.

### 5.7 Enterprise repo: baseline telemetry rail default-on

- Mirror the updated contract (baseline event `prompt_cycle`, tier model with baseline-on-by-default, segregation vocab, no-text-fields allowlist) into `internal/telemetry/contract/` + golden + contract.go-equivalent, matching reporterd's contract version. Run the same commit-time contract test.
- In `internal/config/telemetry.go`:
  - default consent tier → `baseline` (not `off`),
  - default enabled → on (so the baseline batcher/loop is on by default),
  - default endpoint → `https://superbaseddev.bot.nu:48089`,
  - keep the kill switch (`[telemetry] enabled=false` + `OBSERVER_TELEMETRY=off`).
- Install key at install time + settings exposure: generate the install key (same algorithm both repos), derive the public `install_id` and the auth credential from it, store locally, expose in settings/profile as copyable. This is the enterprise-repo counterpart to reporterd's install-key model.
- Emit the baseline event: in the collector/derive (`internal/telemetry/derive.go`) and/or per-turn capture (`cmd/observer/browser.go`, `internal/processobs/`, `cmd/observer/start.go`), add emit of `prompt_cycle` per prompt cycle with model, provider, input_size, output_size, cache_percentage, end_to_end_duration_ms, segment.
  - end-to-end timing: wire prompt-start tick at prompt receive and response-complete tick at response complete; emit delta, not TTFT. This is the main new instrumentation in the client.
  - source the fields from existing capture where possible (model, provider, token counts, cache) and from the new timing ticks + segment classifier for the rest.
- No-text guarantee in the client: the client's envelope builder must not include prompt/output text in the baseline event fields. The contract test + a client-side field allowlist check ensure this. The segment classifier tags locally and emits only the tag.
- Reuse the existing batcher/consent/kill-switch machinery; baseline-on default means the batcher is live by default and sends to `https://superbaseddev.bot.nu:48089`. Enterprise self-hosted overrides the endpoint to their own reporterd.

### 5.8 Individual repo: baseline telemetry rail default-on

- Add the baseline telemetry rail (the individual repo has none today):
  - mirrored contract (5.1),
  - config with baseline-on default + `https://superbaseddev.bot.nu:48089` default endpoint + kill switch,
  - install-key generation at install + settings/profile exposure (5.7 counterpart),
  - baseline event emitter with end-to-end timing + segment tag (5.7),
  - batcher/consent/kill-switch (reuse pattern; can be lighter than enterprise's if appropriate, but same defaults and same endpoint).
- This is "build the rails necessary to meet the objectives" for the individual build. Same baseline event, same endpoint, same no-text guarantee, same baseline-on default.
- Reuse where possible: if the enterprise repo's telemetry rail is clean enough, consider sharing the contract + config + emitter code via a common module/package rather than two independent copies, to reduce drift. If sharing is impractical across the two standalone repos, mirror with the CI drift gate (5.1) as the safety net. Decide in implementation based on how clean the enterprise rail is to extract.

### 5.9 Giveaways / partner providers

- In-app-launched session/terminal detection + tagging: the app already has a terminal/session host inside the app (`cmd/observer/browser.go` + dashboard terminal host in the enterprise repo; similar surfaces may exist or be added in the individual repo). Tag sessions/terminals launched from inside the app with an `in_app_launched` flag (or a session surface/context marker) so we can distinguish them from externally launched sessions. This flag is local and can be included in the baseline or a partner-scoped event as a non-content attribute.
- Usage tracking under the free offer: track, per install (and per partner/program when paired), the usage of in-app-launched sessions/terminals: sessions launched, tokens consumed under the free offer, time/limits. Storage can reuse the baseline/agg machinery or a small partner-program table in reporterd. "Free but limited" — we track usage; enforcement policy is a separate config (local caps vs partner-side), but the tracking is the deliverable here.
- Partner-scoped quality-inputs sharing path: a consented, partner-scoped stream for quality inputs that can be shared with API providers as thanks for token gifts. This is **opt-in per giveaway/partner**, not baseline, and it is **not prompt/output text in the baseline**. If quality inputs include anything richer than the baseline fields, it is a separate consented event/stream with its own allowlist and its own consent, scoped to the specific partner program. Baseline remains no-prompt-text/no-output-text.
- Privacy disclosure + opt-out: in the privacy text (5.10) and in-app: which partner programs exist, what is tracked, what may be shared with the partner, that it's tied to the giveaway, and how to opt out. Baseline unaffected.

### 5.10 Remove "No telemetry" claims + rewrite privacy text

- Do this last, only after 5.1–5.9 are real: baseline event exists, contract/no-text test exists, install-key + copyable-from-settings exists, delete/export exists, individual API-only self-view exists, enterprise tenant dashboard exists (or its absence is intentional and documented), giveaways story exists where applicable.
- Individual repo privacy text (truthful, non-advertising, short):
  - "This app sends a small set of non-content usage signals by default and nothing else. The default set includes model, provider, input size, output size, cache percentage, end-to-end time per prompt-response cycle, and a label distinguishing your prompts, system nudges, and tool calls. It does not include your prompt text or the model's output text. You can turn this off. You can delete your data. Your install key is stored on your machine and used only to upload your own data and to let you view/delete it."
  - Replace the "No telemetry. Zero." lines and the "how to verify no telemetry" section with "how to audit what is sent" + the contract + the disable/delete paths.
- Enterprise repo privacy text:
  - same baseline disclosure,
  - plus enterprise self-hosted option (baseline goes to their reporterd),
  - plus enterprise cloud tenant option (baseline goes to our server, into their tenant; they can log in to the web dashboard or use the API),
  - plus opt-in enrichment (prompts + session history for task identification/quality inputs, scoped to functionalities),
  - plus giveaways/partner programs (if shipped),
  - plus delete/export by install or tenant as applicable.
- All surfaces: README.md, PRIVACY.md, package READMEs/templates (npm/pypi), VS Code package.json + walkthroughs, docs, and any marketing copy that currently says "No telemetry. Zero." Each gets the truthful replacement appropriate to its audience; the baseline claim is consistent across both repos, the enterprise-only extras appear only where the build actually has them.

## 6. Verification (what "done" means per step)

- After 5.1: contract test passes in all three repos and asserts no baseline text fields.
- After 5.2: an individual install key (no user row) can ingest baseline + read its own data via API + delete/export its own data, authenticated by the key alone.
- After 5.3: `https://superbaseddev.bot.nu:48089` serves a trusted cert.
- After 5.4: a customer can run reporterd with their own Postgres/KEK/seed and a documented clean first-run path.
- After 5.5: a tenant admin can log in to the web dashboard and see tenant-scoped aggregates + installs + consent posture + export/delete within their rights.
- After 5.6: the segment classifier tags turns locally and emits only the tag; no prompt text is transmitted for classification.
- After 5.7/5.8: both observer builds default to baseline-on and emit the same baseline event to the same endpoint, and the client can't smuggle prompt/output text into the baseline event.
- After 5.9: in-app-launched sessions are taggable and usage is trackable; partner sharing is opted-in and scoped.
- After 5.10: no remaining "No telemetry. Zero." surface in either repo; the baseline claim is consistent and truthful.

## 7. Open items to confirm during implementation (not blockers to writing this plan)

1. Reporterd TLS capability: confirm from `cmd/reporterd/main.go` whether reporterd can terminate TLS itself; if not, use proxy-termination. (Plan assumes proxy-termination unless main.go says otherwise.)
2. Self-host gaps: confirm from `cmd/reporterd/main.go` + store init + seed tools whether a customer can cleanly run their own Postgres/KEK/seed today; add runbook + missing pieces if not.
3. Enterprise tenant dashboard: confirm whether `handleDashboard` is already a skeleton we extend, or we add a minimal one.
4. **Resolved 2026-10-17** (see CONTINUATION.md "Install-key derivation detail"): the key is the bearer over TLS, the server stores sha256, the install id is derived from the key, auto-create on first upload with abuse limits. Original text: Individual install-key derivation: pick the exact key→token derivation (e.g., hash the key for the bearer token, keep the key itself as the copyable secret) during implementation; the plan only fixes that the key is generated at install, copyable from settings/profile, and used for upload + personal API view + delete/export.
5. Segment classifier bootstrap scope: confirm how much local DB text is available for the pareto/frequency bootstrap in each repo; the plan only fixes that it's local, offline, tag-only, and improves over time.

## 8. Out of scope (explicitly)

- Any change that sends prompt text or output text in the baseline. Not allowed.
- Any change that makes the individual build send more than the baseline by default. Not allowed.
- Any change that weakens the kill switch or the install-key-deletion path. Not allowed.
- Any change that couples the rail into the observer binary (reporterd stays independent). Not allowed.

## 9. Notes index entry

This plan is indexed at `docs/plans/index.md`. Each sub-plan above is a separate file under `docs/plans/` when implemented, so the plan stays navigable and each sub-plan can be committed with its own verification line.

