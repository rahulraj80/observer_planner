# Adding the Daily Briefing — Plan & Status

Single consolidated tracker for the **Daily Briefing** work (the stand-up
product) plus the demo-seeder that feeds it. Every item below lists what we
planned to do and where it stands as of this writing. The authoritative
specs are local-only (gitignored) — read them before continuing:

- `docs/design/standup-briefing.md` — binding spec for the briefing (Phases A/B/C).
- `docs/design/api-intelligence.md` — predecessor "API Intelligence" backbone this builds on.

Status legend: `[x]` done and verified · `[~]` implemented, partially verified ·
`[ ]` not started.

---

## Status summary

| Workstream | Status |
|---|---|
| Phase A — Daily Briefing (no LLM) | `[x]` implemented end-to-end; verified live against seeded data; full `go test ./...` + invariant net green for this stack (remaining suite failures are pre-existing Windows/mirror-only, confirmed at `origin/main`) |
| Phase B — LLM Analysis Connections | `[x]` implemented end-to-end (items 1–6 below) + **keychain & management UI landed (`f681dab`, 2026-08-03)**; live E2E smoke verified against the FreeLLM loopback (`narrative=llm`, `ask`, `analysis_calls` accounting) |
| Phase C — Org manager briefing | `[x]` implemented end-to-end; orgserver/orgcontract/store/config/digest tests green; invariant privacy net + go vet pass; web2 built; live E2E smoke verified; committed `0ca9c8f` |
| Backbone (api-intelligence.md) migrations 074–077 | `[x]` P0 complete (`98a1624`); P1/P2 partially done — see below |
| Demo seeder (`observer demoseed`) | `[x]` done, verified, re-runnable |
| Phase D — Smart briefing + virtuous cycle | `[~]` design captured 2026-08-03 in this tracker; Stage 0 (items 1–3) fully landed; Stage 1 items 1 (weekly/monthly mode) + 2 (smarter opportunities) + 3 (failure analysis) landed; Stage 2 (surface & act) + Stage 3 (measure: 3.1–3.3) landed; **live routing estimate wired (2026-08-05) — p1-v1 estimates now materialize on live proxy turns labeled `usage_basis "live_prompt"`** — see below |

---

## Phase A — Daily Briefing (no LLM) · spec §2/§4

### Product & content model
- `[x]` Pure `internal/briefing` package: types (`types.go`), composer
  (`compose.go`), justification formatter (`justify.go`), table-driven tests
  (`compose_test.go`). Deterministic, no network, no prompt text, no file
  paths; everything injected (`Facts` + pricing fn).
- `[x]` Opportunity detectors: routing-shadow savings, trivial-model
  overprovisioning, low cache hit-rate, long-context surcharge — each with
  `savings_usd` + confidence + a served justification.
- `[x]` Narrative + open questions generated deterministically.
- `[ ]` Section 2.2 "Today, lightly" (in-progress-day strip) — spec marks optional; not implemented.

### Data & store seams
- `[~]` `internal/store/briefing.go` — snapshot seam
  (`SaveBriefingSnapshot` / `LoadBriefingSnapshot` / `LatestBriefingDate`),
  idempotent (newest `generated_at` wins). The **facts loader** was placed in
  `internal/intelligence/dashboard/briefing.go` (one scan of `api_turns` +
  `token_usage`, per-turn pricing via the cost engine) rather than the
  spec's `store.LoadBriefingFacts` — a documented implementation decision.
- `[x]` Migration `078_briefing_snapshots.sql` — `briefing_snapshots(date
  UNIQUE, json, generated_at)`; node-local, never selected by the org-push
  seam (privacy sentinel). Migration `079_analysis_calls` is Part B — done; see Phase B.

### Endpoints (all `V`-class, remote-readable)
- `[x]` `GET /api/briefing?date=&tool=&project=&basis=` — full Briefing JSON.
- `[x]` `GET /api/briefing/opportunities?date=` — flat cards (email/compact).
- `[x]` `GET /api/briefing/justify?id=<dedup_key>&date=` — justification from the persisted snapshot.
- `[x]` `GET /api/briefing/session/<id>` — session drill-down (handler registered).
- `[ ]` `POST /api/briefing/email` — spec §2.6; not implemented (the digest
  CLI covers email; no REST send-now endpoint).
- `[x]` `POST /api/briefing/ask` — Part B; evidence-items-only Q&A, 404
  without a connection (see Phase B).

### UI (solo)
- `[x]` `web/src/pages/Briefing.tsx` — lazy route in `App.tsx`, nav entry
  ("Briefing", first in Monitor) in `lib/nav.ts`, full `web/dist` rebuild
  (standalone + embedded).
- `[x]` Default window = most-recent completed local day ("yesterday"
  shortcut is the default); `?date=` override.

### Daily email (spec §2.8)
- `[x]` `internal/notify/digest/briefing.go` — content-free `ComposeBriefing`
  mirroring the at-a-glance shape, rides the existing email channel.
- `[x]` `cmd/observer/digest.go` — `--kind briefing` runner + `assemble`
  reusing `dashboard.Server.ComposeBriefing` + `digest_state` send-once markers.
- `[ ]` Org runner (`internal/orgserver/digest`) — Phase C.
- `[ ]` `observer digest send --dry-run` manual smoke not yet run (no email transport configured).

### Verification / gates
- `[x]` Live verification against seeded data (see "Demo seeder" below and the Verification log).
- `[x]` Full `go test ./...` (2026-08-02, Go 1.26.5): every briefing-affected
  package passes. Three real gaps found by the run were FIXED: the
  `cmd/observer-org` migrate pin (bumped 21→22 for migration `022`), the
  `/api/briefing/ask` route classification (added to the §9.3 table as `V`),
  and `cmd/observer/attach_resume_test.go` (added `//go:build unix` — it calls
  symbols from the unix-only `attach_client_unix.go`). All remaining failures
  are pre-existing Windows-only or public-mirror gaps (path translation,
  ETW/ConPTY, socket perms, `chmod` semantics, missing `sh`, gitignored
  `docs/guard-rules.md`) — reproduced identically at `origin/main`.
- `[x]` Invariant golden net (`make test-invariant`) run here — green (privacy sentinel included).
- `[x]` `gofumpt`/`golangci-lint` — **tools installed, lint gate closed** (commit `3e40ce2`). Only pre-existing findings remain (linuxebpf unused fields, Windows-only test helpers called from `//go:build unix` files).

---

## Phase B — LLM Analysis Connections (opt-in, never load-bearing) · spec §3

Status: implemented end-to-end; the shared client's design decisions are
recorded in the Open decisions table below (Q2 separate `analysis_calls`
table, Q3 evidence-items-only, Q6 migrate judgeclient first).

The spec §3.4 management interface (keychain secret refs + management UI —
deferred in an earlier pass) landed 2026-08-03 in `f681dab`; the Phase B
live E2E smoke was verified the same day (see Verification below).

- `[x]` `internal/llmconn` package — connection model + `Local()`/`Remote()`/
  `usable`, pure task-based `Select` (hint wins → local preferred → first
  enabled), shared `Client` for the anthropic / openai_compat /
  local_openai_compat dialects (60s timeout, judge-style retry, scrub-before-
  egress for remote, max prompt bytes, `X-SBO-Analysis` + `X-Title`
  attribution, `WithSystem`/`WithHTTPClient` seams), health `Test`.
- `[x]` `[llm.connections.<id>]` static TOML config (`internal/config/llm.go`):
  env-ref secrets only (literal secret rejected), closed kind/role
  vocabularies, enabled-remote requires `allow_remote=true` + key ref,
  `ToConnections()` → sorted `[]llmconn.Connection`. Keychain refs + the
  management layer landed 2026-08-03 in `f681dab` (see the two bullets below).
- `[x]` Migration `079_analysis_calls.sql` (content-free accounting rows:
  ts / connection_id / model / task / tokens / cost / latency / status) +
  `internal/store/analysiscalls.go` (`RecordAnalysisCall` / `ListAnalysisCalls`);
  table excluded from org push (privacy sentinel updated).
- `[x]` `observer llm` CLI group (`list` / `setup` / `status <id>` / `add` /
  `remove <id>`): loopback auto-discovery reuses `probeLocalModelServers`
  (Ollama :11434, vLLM/llama.cpp :8000) + `Client.Test`; never touches remote.
- `[x]` Narrative upgrade: `GET /api/briefing?narrative=llm` (opt-in, only with
  a runner wired; failure keeps the deterministic template + logs). New
  `POST /api/briefing/ask` — evidence-items-only Q&A over the briefing facts;
  404 without a connection, 400 on bad body, 502 on call failure.
  `internal/llmrun` orchestrates (template-kept-on-failure, content-free
  prompts, `analysis_calls` accounting).
- `[x]` Migrate hand-rolled LLM callers onto the shared client (behavior-
  preserving): **judge** (`cmd/observer/judgeclient.go`, shared by obs
  eval + admission — same credential/model gates, retries, num_ctx gating,
  egress scrub) and **`intelligence/summary`** (Anthropic Messages via llmconn;
  same `ANTHROPIC_API_KEY` default, `BaseURL`/`Model`/`MaxTokens` knobs,
  scrub-at-storage posture; gains judge-style retry + attribution).
  `messagesummary` + guard-cloud remain on their own clients (follow-up).
- `[x]` **Keychain secret refs** (`f681dab`): new `internal/llmkeyring`
  package — OS keychain or 0600-file fallback (Service `superbased-observer-llm`).
  `PUT/DELETE /api/config/llm/key/<id>` stores the literal ONLY in the
  keychain and keeps `api_key_keychain = "<id>"` in config.toml (never the
  literal); `llmrun.Runner.Keychain` + `observer llm status` resolve the ref
  at use time. Root bug fixed along the way: `config.ToConnections()` now
  copies `APIKeyKeychain` (was silently dropped).
- `[x]` **Management UI (solo)** (`f681dab`): Settings → "LLM analysis"
  section — connection fields (name/kind/base_url/default_model/models/
  task_roles/api_key_env/limits/enabled/allow_remote), save via the standard
  settings-section mechanism (`PUT /api/config/section/llm`, `llm` added to
  `editable_sections`), **Test** button (`POST /api/config/llm/test/<id>` —
  health + latency + model list), and a key-save/delete field.
  **Deviation vs spec §3.4 (decided Q7):** the UI edits the STATIC TOML
  `[llm.connections.<id>]` block rather than a runtime `llm_connections`
  table + `/api/llm/connections` CRUD; `GET /api/llm/status` and the org
  (web2) page are not built. Kept the TOML-edit approach — it reuses the
  section-save path (validation + `.bak`) and keeps one source of truth.

### Verification (Phase B live E2E — FreeLLM loopback `:3001`, 2026-08-03)
- `[x]` `observer llm status freellmapi` — `ok (99 models)` (~300ms); the
  web-saved keychain key resolved and authenticated against the live server.
- `[x]` `GET /api/briefing?narrative=llm` — template (218 ch) upgraded to a
  grounded LLM narrative (370 ch): "7 sessions / 1315 turns / $3.70 /
  opencode 1181 turns / claude-code $1.94 / routing-shadow $7.60" — all
  numbers match the facts.
- `[x]` `POST /api/briefing/ask` — "Which tool cost the most…?" → grounded
  answer (claude-code $1.94; routing_shadow reroute $7.60),
  `connection_id: freellmapi`, ~10s.
- `[x]` `analysis_calls` accounting — 5 rows, all `status=ok`, model
  `openai/gpt-oss-20b:free`, $0 list-basis (free model); rows carry
  ts/conn/task/input+output tokens/latency_ms (e.g. ask in=486 out=208 6.6s;
  narrative in=483 out=753 25–77s).

---

## Phase C — Org manager briefing · spec §2.6/§2.8

Implemented end-to-end. The design decision Q4 (hash-only digest vs exclude)
was **DECIDED 2026-08-02: hash-only digest** — see the Open decisions table.

- `[x]` `internal/orgserver/rollup/briefing.go` — `Briefing(ctx, db, day, scope,
  now)`: team-scoped composed briefing over the org server's own `api_turns` +
  `token_usage` (same union + proxy-origin dedup as the node loader), recorded
  costs, `internal/briefing` composer, baked-in cost engine only for the
  counterfactual reprice cards. `BriefingAggregates(ctx, db, day)` — admin feed
  over the pushed `briefing_aggregates` table. Table-driven tests
  (`briefing_test.go`): admin scope, lead scoped to own team, empty-day zero,
  aggregates window/ordering.
- `[x]` Handlers `internal/orgserver/dashboard/briefing_handlers.go` — manual
  routes in `server.go` buildMux (no codegen; org spec is gitignored here):
  `GET /api/org/briefing[?date=YYYY-MM-DD]` (role-scoped via `a.caller`,
  default = most recent completed UTC day) and `GET /api/org/briefing/aggregates`
  (admin-only, wraps `{date, aggregates}`).
- `[x]` Org-server migration `022_briefing_aggregates.sql` — `briefing_aggregates`
  table, PK `(org_id, user_email, day, tool, project_hash)`, UPSERT by natural key;
  ingest upsert loop + `forcePusherEmail` re-pin. `db_test.go` bumped to v22.
- `[x]` Node-side hash-only aggregate: `internal/store/briefingorg.go`
  `SelectBriefingAggregates(ctx, 7)` — per (completed local-day, tool,
  project_hash) counts + sums from `api_turns`+`token_usage`, proxy-origin
  dedup, never reads `briefing_snapshots`/`analysis_calls`. `orgpush.go`
  attaches it under `ShareOptions.BriefingAggregates`; `orgcontract` carries
  `PushEnvelope.BriefingAggregates` (golden pinned). Node toggle:
  `[org_client.share] briefing_aggregates` (default false, own consent).
  `internal/orgclient/client.go` forwards it. Privacy sentinel updated.
- `[x]` Daily manager email: `internal/orgserver/digest/digest.go` now
  parameterized by `kind` ("org" | "briefing") — `NewBriefingScheduler` sends
  the composed admin-scope briefing for the last completed UTC day via the
  digest state marker; `cmd/observer-org/digest.go` gains
  `digest send --kind org|briefing [--dry-run]`.
- `[x]` `web2/src/pages/Briefing.tsx` — org briefing page (date picker, KPI
  grid, activity table, top sessions, expandable opportunity cards with
  embedded drill-down math, questions, and the admin-only aggregate digest
  table). Route `/briefing` in `App.tsx`, sidebar entry under Optimize;
  `api.ts` `briefing()` / `briefingAggregates()` + wire types. `web2` + embedded
  `dist` rebuilt.
- `[x]` Live end-to-end smoke (2026-08-02): org server on :9443 (schema v22,
  dev-auth) + IdP metadata server; enrolled node pushed 948 rows (36 KB)
  including briefing aggregates; `/api/org/briefing` 200 (manager digest for
  2026-08-01), `/api/org/briefing/aggregates` 200 admin-only (lead 403),
  `digest send --kind briefing --dry-run` composed; re-push idempotent (0 rows).
  Full verification log in the E2E workspace (see briefing-e2e in TEMP).
- `[x]` Committed as `0ca9c8f` (Phase C changeset).
- `[x]` `gofumpt`/`golangci-lint` — **tools installed, lint gate closed** (commit `3e40ce2`). Only pre-existing findings remain.

---

## Backbone (api-intelligence.md) — P0 complete

- `[x]` Migration `074_turn_kind.sql` + `internal/turnkind` (offline classifier, table-driven tests).
- `[x]` Migration `075_license_usage.sql` + `internal/licenses` (config, cost model, break-even, store seam).
- `[x]` Migration `076_dispatch_benefit.sql` — benefit_json/text, entitlement_kind, effective_basis, realized_delta_usd on router_decisions.
- `[x]` Migration `077_cross_provider.sql` — provider columns on router_decisions + api_turns.
- `[x]` `internal/evidence` — historical evidence substrate (SelectEvidence, Summarize).
- `[x]` Model Fit page (`/modelfit`) — kind×model×provider matrix with cost/turn, error rate, latency, cache.
- `[x]` API Explorer page (`/apiexplorer`) — provider×model×kind×time cuts with filters.
- `[x]` Licenses page (`/licenses`) — inventory, utilization bars, quota, break-even.
- `[x]` Nav entries + icons for all three new pages.
- `[x]` Dashboard endpoints: `/api/evidence/summary`, `/api/evidence/matrix`, `/api/evidence/timeseries`, `/api/license/usage`.

Remaining (P1/P2):
- `[x]` `observer kinds backfill` CLI (uses internal/turnkind).
- `[x]` `routing/benefit.go` — pure expected-benefit record (§5.4): `QualitySignal`
  enum, `BenefitBreakdown`, `ComputeBenefit` (p1-v1 math, mirrors switchEconomics),
  `BenefitText` (human sentence); table-tested. Wired end-to-end: `Decision` gains
  `Benefit`/`BenefitText`/`Entitlement`/`EffectiveBasis`; engine attaches benefits at
  every committed switch site (cache-holds carry none — their negative net stays in
  `EstSavingsUSD`); store seam persists `benefit_json`/`benefit_text`/
  `entitlement_kind`/`effective_basis`/`realized_delta_usd` (round-trip tested);
  `decisionRow` maps them; `routing explain` surfaces the benefit sentence + basis +
  realized delta, and `/api/routing/decisions` serializes the new fields via
  `RouterDecisionDetail` (no handler change). `RecordServed` writing
  `realized_delta_usd` — **DONE** (Phase D Stage 0, item 1); quality-signal
  injection from the `evidence_quality` basis — **DONE** (Phase D Stage 0, item 2).
  **LIVE estimate landed (2026-08-05)**: the engine now prices the coarse
  prompt-side bundle `{Input: Shape.PromptTokens}` on the live hot path (the same
  fallback `costMinimizeBasis` already ranked on), so p1-v1 estimates materialize
  on live proxy turns instead of staying 0; the benefit record labels the basis
  `usage_basis "live_prompt"` (vs `"realized"` on replay), and the §R13 cache-hold
  requires replay-exact pricing — a coarse live estimate never blocks a switch
  (no regression from the pre-estimate behavior). No proxy seam change was
  needed: the proxy already fed `PromptTokensEstimate` into `Shape.PromptTokens`.
- `internal/proxy/translate` — cross-provider shape translation (P2, off by default).

## Route classification regression (fixed 2026-08-03)

The backbone P0 commit (`98a1624`) registered `/api/evidence/{summary,matrix,
timeseries}` and `/api/license/usage` as `V` routes but never added them to the
§9.3 route-to-capability table (`remote_local_class_test.go`). `TestFullRegistry-
Classification` caught it during the 2026-08-03 stock-take; fixed in `ef0d630`
(all four are read-only aggregate views). Remaining dashboard-suite failures
(ETW, redactor paths, config PUT races, raw-events JSONL) reproduce identically
at `origin/main` — pre-existing Windows-mirror artifacts, not ours. `go vet`
clean except the pre-existing `unsafe.Pointer` note in
`internal/termsession/spawn_windows.go` (untouched since `506d359`).

---

## Demo seeder (`observer demoseed`) — done & verified

Files: `internal/demo/seed.go` (generator), `cmd/observer/demoseed.go`
(command), `internal/demo/seed_test.go` (smoke + idempotency + priceability),
`cmd/observer/main.go` (registration).

- `[x]` Deterministic (`rand.NewSource(0xC0FFEE)`); newest generated day is **yesterday**.
- `[x]` 6 projects, 30 named users, ~75% of workdays agentic coding across
  the top harnesses (claude-code/codex proxy, cursor/gemini-cli/opencode
  watcher) + chat/batch harnesses; ≤3 req/min cap.
- `[x]` Top-30 priceable models; stored costs are 0 by design — every surface
  reprices through the cost engine (`SeedResult.EstimatedUSD` computed only for the CLI summary).
- `[x]` Deliberate patterns so opportunity detectors fire: low-cache days
  (Wed+Sun), long-context surcharges, trivial-model sessions, routing-shadow
  candidate turns, 3% error turns.
- `[x]` Purge-safe: re-seeding deletes only `demo-seed` / `seed-%` / `/work/*` rows, FK-ordered.
- `[x]` `observer demoseed [-d days=7] [-u users=30 max]` prints a JSON summary.

### Verification log (live dashboard, PID 7140 on :8080)
- `[x]` `/api/briefing` 2026-07-30 (weekday): 419 sessions, $228, cache 62%,
  routing-shadow + long-context + trivial cards, no false cache card.
- `[x]` `/api/briefing` 2026-07-29 (low-cache Wed): cache 31%, cache card fires (~$50.49).
- `[x]` `/api/briefing` 2026-08-01 (Sat, default/yesterday): 49 sessions, $19.62.
- `[x]` `/api/briefing/justify?id=routing_shadow_1` — evidence + math served from snapshot.
- `[x]` `/api/cost?days=7` reprices stored-0 costs: $1,307.63 / 53,877 turns across 38 models.
- `[x]` Final run: 2,111 sessions / 20,806 api_turns / 34,512 token_events /
  44,300 actions / 124 router_decisions, est. $1,379.41.

## Phase D — Smart briefing + the virtuous cycle (intelligence loop)

Design captured 2026-08-03. Nothing here is started yet. Phase B/C landed the
opt-in LLM plumbing (connections, keychain, `analysis_calls`, narrative + Q&A,
org rollup) and Phase B verification proved the narrative is grounded. Phase D
turns the briefing from a *passive report* into the **pull surface of a closed
optimization loop**. Because we already sit on the proxy path (and watch every
adapter), we have both the data and the control plane ourselves — no external
analytics SaaS (Grafana / Claude-dashboards / Helicone-class) is required for
any of this. All of it reuses our own store + cost engine + composer.

### The loop we want

```
                     ┌────────────────────────────────────────────┐
                     │  STORE  (api_turns, router_decisions,      │
                     │  token_usage, cache, guard, analysis_calls)│
                     └──────────────┬─────────────────────────────┘
                          ▲         │  every call is sensed here
      Measure             │         ▼
   (realized benefit)     │    ┌──────────┐   Learn (detectors,
   reported next day      │    │  LEARN   │   evidence, LLM read)
     ┌────────────────────┴───▶│          │───────────┐
     │                          └──────────┘           │
     │                                                 ▼
     │                                          ┌──────────┐
     │    PROXY + ROUTING                        │ SURFACE  │  Briefing cards:
     │    (policy applied) ◀──────  Act  ◀───────│ (CTA)    │  opportunity + $saved
     └──────────────────────────  (config       └──────────┘  + "Apply" button
                                  write)                    + simulate-first preview
```

1. **Sense** — proxy + watcher record every turn (tokens, cost, cache,
   latency, model, routing decision + expected benefit, guard verdict).
2. **Learn** — analytics layer (briefing detectors + evidence + LLM read)
   converts raw rows into *opportunities with quantified savings*.
3. **Surface (CTA)** — the briefing card grows an **Apply** action, not just
   "drill down": simulate-first preview of the change, one click to land it.
4. **Act** — the CTA writes the routing/config change (existing
   `routingapply`/`routingconfig`/settings-section machinery; restart-honest
   or live via the proxy seam).
5. **Measure** — the proxy records the *realized* benefit
   (`router_decisions.realized_delta_usd`); the next briefing reports
   **estimated vs realized** for every change you applied.
6. **Reinforce** — "you applied X on DATE, it saved $Y of $Z estimated" is the
   pull that brings the user back — a self-reinforcing virtuous cycle and a
   built-in quantified-ROI story. LLM coaching notes add the human "why".

The loop is **data-only** until the user opts in: everything is locally
computed; LLM reads/notes only run when a connection is configured (Phase B
machinery), and never fabricate numbers (template-kept-on-failure invariant).

### Stage 0 — foundations (unblocks measurement, small)

- `[x]` **Realized-benefit measurement** — the proxy passes the landed turn's
  usage bundle to the `ModelRouter` so `RecordServed` writes
  `router_decisions.realized_delta_usd`. The column already exists (migration
  082); the `ModelRouter` seam is now wired: `RecordServed(token, apiTurnID,
  servedModel, usage proxy.RouterUsage)` (a plain proxy-local mirror of
  `CostTokens`), `realizedDelta` prices the landed bundle at the original
  (un-routed counterfactual) and the actually-served model on the same p1-v1
  basis, and the row carries it at insert time. Nil unless a real model change
  happened AND both models are priceable AND usage landed (advise rows, dated
  echoes of the original, zero-usage turns, unpriceable served models all stay
  nil — no invented dollar claims). Tests: `TestLiveRouter_RealizedBenefit*`
  (cmd) + the proxy seam's usage-bundle assertion. This is the "did it actually
  help" number the loop measures. **The estimate side is now live too
  (2026-08-05): the engine prices `{Input: Shape.PromptTokens}` when
  `ObservedUsage` is nil — the same fallback `costMinimizeBasis` already ranked
  on — so the decision row carries est_savings_usd + benefit_json labeled
  `usage_basis "live_prompt"` next to the realized delta. The §R13 cache-hold
  gate stays replay-exact-only (a coarse live estimate never holds), and the
  prompt-side estimate is a deliberate lower bound on gross (output/cache
  dollars land only once the response does). Stage 3's realized-vs-estimated
  now has both sides on the same row.**
- `[x]` **Benefit confidence** — the `evidence_quality` basis is now wired
  into `ComputeBenefit`: `Snapshot.Evidence` carries a `(turn-kind, target
  tier) → QualityEvidence{Signal, Confidence}` map derived from the
  calibration cells (`model_calibration`, node-level `project_id 0` pools),
  and `attachBenefit` reads it at every committed switch so estimates carry
  a confidence that compounds toward 95% as evidence accumulates
  (`internal/routing/evidence.go` pure derivation, `internal/store`
  slow-tick pooling). Design note: implemented as a `Snapshot.Evidence`
  map read by `attachBenefit` rather than the spec's new `evidence_quality`
  routing basis — no new basis added. Committed 2026-08-04 (see git log).
- `[x]` **Standard reports, self-hosted** — no external tooling: latency
  percentiles (TTFT) per model/provider/tool, cache-money-saved trend, error
  rate by model, from existing `api_turns`/`token_usage`/`router_decisions`
  (Grafana-class output, built on our own store). Feeds the briefing's
  numbers section and the org rollup. Landed: (a) the proxy now captures
  real TTFT (`streaming.go` tees the first-chunk timestamp into
  `api_turns.time_to_first_token_ms`); (b) `internal/evidence` repaired to
  the real schema with true nearest-rank percentiles; (c) new
  `/api/reports/latency|errors|cache` endpoints; (d) the headline
  (/api/analysis/headline) now carries `latency` + `errors` numbers;
  (e) Model Fit (`/api/evidence/matrix|timeseries`) fixed to the real
  timing columns. The org rollup already reads the real timing columns
  (`apiTurnPercentile` on `time_to_first_token_ms`/`total_response_ms`),
  so the rollup seam is live. Committed 2026-08-04 (see git log).

### Stage 1 — Learn (analytics depth, LLM-assisted)

- `[x]` **Weekly / monthly briefing mode** — multi-day rollup + trend deltas
  (spend, tokens, cache, latency, errors) with the same grounded narrative;
  `?window=` beyond a single day. Landed 2026-08-04:
  `GET /api/briefing[?window=1d|7d|28d|weekly|monthly|N[d|w]]`. Window =
  the last N COMPLETED local days (never an in-progress day); `?date=` anchors
  the END day. The composer emits `period` (day|week|month|N-day) and a
  `trend` block (only when `window` > 1d AND a same-length prior window has
  activity) with spend/turns/sessions/tokens/cache/error-rate/TTFT-p95 deltas
  vs the prior window, and the narrative gains the "vs the prior N days"
  comparison. Real latency entered the pipeline too: the facts loader now
  scans `api_turns.time_to_first_token_ms`/`total_response_ms` (per-turn
  `Turn.TTFTMS`/`TotalMS`; positive captures only) and `AtGlance` carries
  nearest-rank `ttft_p50/95_ms` + `total_p50/95_ms`. Multi-day briefings are
  computed on demand and never write the per-day snapshot (date-UNIQUE table);
  `/opportunities` + `/justify` + `/ask` accept `window` and recompose on
  demand. UI (`web/`) toggle is a follow-up — requires a Node rebuild, not
  run on this box.
- `[x]` **Smarter opportunities** — cross-day correlation of detector hits,
  LLM prioritization/grouping, and "this pattern recurs every low-cache day"
  reasoning over the stored facts. Landed 2026-08-04, with the LLM half landed
  2026-08-05 (`POST /api/briefing/triage`, below): the composer now
  runs multi-day recurrence detectors in `internal/briefing/recurrence.go`
  (`Facts.WindowDays > 1` only). `recurring_cache_miss` fires when cache hit
  rate was below the healthy threshold on ≥ max(2, window/7) days, prices the
  would-be-warm reads per-day (gap × day-tokens × blended input rate × 0.5,
  summed over the affected days), names the recurring weekday when ≥ ⌈⅔⌉ of
  the affected days cluster on one, and carries the affected local days on
  `Justification.Days`. `recurring_errors` fires when error turns span ≥ 2
  days of a ≥ 7-day window. `day_correlation` names days that were BOTH
  low-cache and error-laden. Cards set `Opportunity.Recurrence=true` and are
  never suppressed by the `MinSavingsUSD` dollar filter (a recurrence card
  with $0.02 savings still surfaces). All three are pure/deterministic with
  testdata-level tests (`recurrence_test.go` + a 28-day endpoint test that
  seeds a cold-cache-every-Wednesday pattern). LLM follow-up (landed
  2026-08-05): `POST /api/briefing/triage` runs the `triage` task over the
  composed card set (shared `?date=`/`window=`; max 30 cards) — the model
  reorders the cards, groups them, and summarizes; the reply `order` is
  validated to a full permutation of the card set (unknown keys/duplicates
  dropped, omitted cards appended in input order), group keys are validated,
  and a zero-card set answers deterministically WITHOUT egress. API + tests
  only — web follow-ups below.
- `[x]` **Failure analysis** — dedupe recurring `error_turns` across sessions,
  LLM diagnosis, "try this" (ties into `advisor/learn`). The dedupe + "try this"
  half landed 2026-08-04 (`8da7357`): `detectFailurePatterns` groups error
  turns by `(category | normalized-message | tool | model)` over
  `api_turns.error_message` (scrubbed, now scanned by the briefing facts
  loader), surfaces only patterns spanning ≥ 2 sessions/days as
  `failure_pattern` cards (`Category=reliability`, `Recurrence=true`,
  ranked by occurrence count), and serves categorized nudges via
  `failureNudge` (timeout/compilation/test_failure/permission/runtime). The
  `failure.Normalize` helper was exported so the dedup key uses the same
  canonicalization as `failure.CommandHash`. LLM diagnosis of the pattern
  landed 2026-08-05 (`POST /api/briefing/diagnose` — the `diagnose` task over
  `AggregateFailurePatterns`: per-pattern grounded evidence + "try this",
  max 5 patterns, out-of-range/duplicate handles dropped, zero-pattern case
  answers deterministically WITHOUT egress). API + tests only.
- `[x]` **Ask-your-past** — natural-language archaeology over the session
  summaries we already persist ("what did I do on last Tuesday's session…?"),
  grounded, evidence-only. Landed 2026-08-04: FTS5 index over
  `sessions.summary_md` (migration 084, sync triggers + backfill),
  BM25-ranked retrieval seam `store.SelectPastSessions` (occurrence-count
  primary, AND-semantics MATCH, trailing-year window, project filter, top
  recurring `api_turns.error_message`s as failure context), the `ask_past`
  LLM task (`Runner.AskPast` with a prompt that reduces project roots to a
  bare repo marker and never carries raw transcripts), and
  `POST /api/briefing/ask-past` (404 no_connection / 400 decode / 502
  upstream; zero matching sessions answer deterministically WITHOUT egress).
  API + tests only — no CLI or web UI (per the open-decision scope).
- `[x]` **Coaching notes** — LLM micro-feedback on the user's own usage
  ("you retried the same failing test 8× across 4 sessions…"). Landed
  2026-08-04: the deterministic evidence bundle is the exported
  `briefing.AggregateFailurePatterns` (the same multi-session/day aggregation
  behind the `failure_pattern` cards) + `learn.Derive` failure→recovery rules
  (retry volume, recoveries, edited files), the `coach` LLM task
  (`Runner.Coach` with a prompt that reduces project roots to a bare repo
  marker and edited-file paths to basenames), and `POST /api/briefing/coaching`
  (404 no_connection / 400 decode / 502 upstream; zero evidence answers
  deterministically WITHOUT egress). Also closed the config gap:
  `ask_past`/`coach` are now first-class `task_roles` vocabulary (both were
  rejected at load before). API + tests only — no CLI or web UI (per the
  open-decision scope).

### Stage 2 — Surface & Act (call-to-action)

- `[x]` **Apply CTA on opportunity cards** — simulate-first preview (projected
  monthly savings via the cost engine reprice) then one-click land the change
  through `routingapply`/settings-section, with undo/rollback. Landed
  2026-08-04: `internal/configapply` (LeverForDetector → the ONLY lever with a
  real write path today, `routing_shadow` → `[routing].policy`; `BackupFile`
  stamped `*.bak-observer-cta-<stamp>` pre-write snapshots; `RestoreFile`;
  JSONL undo ledger next to the DB — `RecordApply`/`RecordRevert`/
  `LatestApply`/`AppliedBackups`, mirroring the routingapply ledger posture).
  The shadow report now carries the real decide-under policy name
  (`AdviseShadowReport.PolicyName` — dominant `router_decisions.policy_name`,
  tie-break alphabetical) instead of the "candidate" hardcode, so the apply
  writes an actual template name. Endpoints: `POST /api/briefing/cta/plan`
  (simulate-first — re-derives the card + re-reads the shadow signal, returns
  the exact `Change{section,key,from,to,restart_required}`, `already_active`,
  and projected monthly = 24h shadow savings × 30; 404 unknown card / no
  active shadow); `POST /api/briefing/cta/apply` (Local — re-derives + checks
  `already_active` UNDER `configWriteMu`, stamped backup → `writeConfigToml`
  → `config.Validate` → ledger; 409 already-active / no config path, 422
  detector has no lever); `POST /api/briefing/cta/revert` (Local — `{section,
  key}` restores the newest recorded snapshot, `{all:true}` the full unwind,
  restoring the LEDGER-recorded path); `GET /api/briefing/cta/ledger` (View).
  Routing binds at daemon start → apply reports `restart_required`. API +
  tests only — no web card action slot yet (the web deep-link to
  RoutingApplyCard stays a follow-up).
- `[x]` **Routing CTA** — routing-shadow / model-choice opportunities write
  into the actual routing policy, not just a suggestion. Landed 2026-08-04
  (this commit, on top of item 1): the routing_shadow CTA grows from a single
  lever to a **multi-lever plan**. `internal/configapply.LeversForDetector`
  now returns the full lever set (mode / policy / stickiness) and every
  `Change`/`Lever` carries a stable `ChangeID` + `AlreadyActive`; the shadow
  report gains `StickinessHolds` + `StickinessHoldSavingsUSD` (held decisions
  priced at the mean reroute savings — the coherence floor is the reason the
  shadow's cheaper choice didn't land, so the honest lever is to LOWER the
  floor). Plan semantics: **mode** (advise→enforce — the primary lever that
  realizes the shadow's reroutes, the shadow runs in advise mode), **policy**
  (swap to the shadow's decide-under template; skipped when the shadow policy
  isn't a known template), **stickiness** (lower
  `[routing].stickiness.min_turns_between_switches` by 1, floor 1, only when
  holds exist). `POST /api/briefing/cta/apply` now takes `change_id` (400
  missing / 404 unknown / 409 already-active) and re-derives the target value
  UNDER `configWriteMu` per lever. Design notes captured in open decision 9.
  API + tests only — web card action slots for the new levers stay a follow-up.
- `[x]` **Pull rail** — an "Actions recommended" rail on the briefing linking
  each CTA to the config surface + expected impact, so the briefing reads as
  "here's what to do next", not a report. Landed 2026-08-04 (this commit):
  `GET /api/briefing/actions?date=&window=` (pure read, View-class) composes
  the briefing once and emits one row per active opportunity card. Levered
  cards (routing_shadow) reuse the exact CTA plan builder
  (`ctaPlanForCard`) so each non-already-active lever surfaces as an
  actionable row carrying `config_surface` (section.key), `change_id` (the
  CTA-apply selector), `from`→`to`, `restart_required`, and
  `projected_monthly_usd` — guaranteeing the rail's config write + impact
  can never drift from what `/api/briefing/cta/apply` will actually do.
  Cards with no honest config lever (cache/long-context/trivial/recurrence/
  failure) render as `actionable=false` guidance rows, so the rail covers the
  WHOLE card set. Sorted actionable-first by projected monthly, then guidance
  by savings. A levered detector whose every lever is already active falls
  back to a guidance row (never offers a no-op write). API + tests only — the
  web rail UI (a `web/` card-deck) stays a follow-up.

### Stage 3 — Measure & reinforce (closes the loop)

- `[x]` **Realized-vs-estimated reporting** — briefing tracks every applied
  change (`realized_delta_usd`) and reports, next day and in weekly mode:
  "applied X on DATE → saved $Y of $Z estimated, confidence before/after".
- `[x]` **Virtuous-cycle trend** — "changes applied → savings realized" as a
  cumulative adoption/benefit trend the user returns to see.
- `[x]` **Exec memo (org plane)** — manager-facing summary with every figure
  cite-able to a recomputable query; reuses the org rollup + digest channels.

---

## Outstanding items / next steps

1. `gofumpt`/`golangci-lint` — **DONE** (commit `3e40ce2`; tools installed; gate closed).
2. Manual smoke of `observer digest --kind briefing` against a test inbox (Phase A email).
3. Phase B live end-to-end smoke — **DONE** (2026-08-03): `observer llm status
   freellmapi` ok (99 models); `GET /api/briefing?narrative=llm` grounded
   narrative; `POST /api/briefing/ask` answered with `connection_id:
   freellmapi`; `analysis_calls` rows recorded. Ran against the FreeLLM
   loopback (`:3001`) using the web-saved keychain key; an Ollama run remains
   optional (same code path, `probeLocalModelServers` already covers it).
4. Backbone (api-intelligence.md) migrations 074–077 + Model Fit / API Explorer / Licenses pages (P0) — **DONE** (commit `98a1624`).
5. Remaining P1/P2 backbone: `routing/benefit.go` **implemented and surfaced**
   (§5.4 expected-benefit record, pure + engine + store + decisionRow; `routing
   explain` + `/api/routing/decisions` carry `BenefitText`). **LIVE estimate
   DONE (2026-08-05, this commit)**: the live proxy path now prices the coarse
   `{Input: Shape.PromptTokens}` bundle (the `costMinimizeBasis` fallback), so
   p1-v1 `est_savings_usd`/`benefit_json` materialize on live rows labeled
   `usage_basis "live_prompt"`; the §R13 cache-hold requires replay-exact math
   and never fires off a live estimate (no routing-behavior regression). The
   remaining P2: `internal/proxy/translate` (cross-provider). `RecordServed`
   writing `realized_delta_usd` — **DONE** (Phase D Stage 0, item 1). The
   `evidence_quality` basis → `ComputeBenefit` wiring — **DONE** (Phase D
   Stage 0, item 2; snapshot `Evidence` map, commit 2026-08-04).
6. Phase D — smart briefing + virtuous cycle — **designed (this tracker)**;
   Stage 0 items 1 (realized-benefit measurement), 2 (benefit confidence),
   and 3 (self-hosted standard reports) landed (commits 9c2e0da /
   benefit-confidence commit / standard-reports commit, 2026-08-04). Stage 0
   is fully done. **Stage 1 item 1 (weekly/monthly briefing mode) landed**
   (commit 2026-08-04: `?window=` multi-day rollup + trend deltas + real TTFT
   latency percentiles in the facts loader; multi-day never snapshots).
    **Stage 1 item 2 (smarter opportunities) landed** (commit 2026-08-04:
    cross-day recurrence detectors — recurring cache miss w/ weekday naming +
    reproducible per-day savings, recurring errors, day correlation; LLM
    prioritization/grouping of the card set landed 2026-08-05 via
    `POST /api/briefing/triage`). Next
    Stage 1 items: failure analysis (**landed 2026-08-04, `8da7357`** — the
    dedupe + "try this" half; LLM diagnosis landed 2026-08-05 via
    `POST /api/briefing/diagnose`),
   ask-your-past (**landed 2026-08-04** — FTS5 retrieval over persisted
   session summaries + `POST /api/briefing/ask-past`),
   coaching notes (**landed 2026-08-04** — `coach` LLM task over the
   `failure_pattern` + failure→recovery evidence, `POST /api/briefing/coaching`).
   Stage 1 is now fully landed; next is Surface→Act→Measure. See the Phase D
   section above.
7. Phase D Stage 2 item 1 (Apply CTA on opportunity cards) — **DONE**
   (2026-08-04, this commit): `internal/configapply` (detector→lever map,
   stamped pre-write backups, JSONL undo ledger) + `POST /api/briefing/cta/
   {plan,apply,revert}` + `GET /api/briefing/cta/ledger`; the shadow report
   now carries the real decide-under policy name (the "candidate" hardcode is
   gone). Web card action slot + the Routing CTA tier-placement lever remain
   Stage 2 follow-ups (items 2–3 below).
8. Phase D Stage 2 item 2 (Routing CTA) — **DONE** (2026-08-04, this commit):
   the routing_shadow CTA is now a multi-lever plan — mode (advise→enforce),
   policy (template swap), stickiness (lower coherence floor) — via
   `configapply.LeversForDetector` + `change_id` apply selection; the shadow
   report gains the stickiness-holds savings breakdown. Web card action slots
   for the new levers remain a Stage 2 follow-up.
9. Phase D Stage 2 item 3 (Pull rail) — **DONE** (2026-08-04, this commit):
   `GET /api/briefing/actions` surfaces every active opportunity card as a
   "do this next" row (levered cards → actionable rows with `config_surface` +
   `change_id` + projected monthly, reusing `ctaPlanForCard`; guidance-only
   cards → `actionable=false`). Web rail UI remains a follow-up.
10. **Phase D Stage 3 item 1 (Realized-vs-estimated reporting) — DONE** (this
    commit): `internal/briefing` pure detector `detectRealizedVSEstimated` +
    `RealizedConfidence` / `applyConfidence` helpers + `Opportunity.Report`
    flag to exempt the card from the dollar filter; loader in
    `internal/intelligence/dashboard/briefing.go` reads the briefing-CTA ledger
    and queries `store.SelectRealizedRoutingDelta` (mode/policy-filtered so
    pre-restart / old-config turns never inflate the realized figure); card
    renders "Applied routing.mode on 2026-08-01 → saved $0.07 of $0.10
    estimated" with before/after confidence in evidence. New store seam
    `SelectRealizedRoutingDelta` + unit tests; composer + loader tests added.
11. **Phase D Stage 3 item 2 (Virtuous-cycle trend) — DONE** (this
    commit): `internal/briefing` pure detector `detectVirtuousCycle` reads
    `Facts.VirtuousCycle` (cumulative history); loader `virtuousCycleFacts`
    in dashboard reads ALL ledger applies (not window-limited), finds earliest
    apply, and queries cumulative `SelectRealizedRoutingDelta` from that point
    to now (broad filter avoids double-count); weekly trend in `ByWeek` shows
    applies + projected per week, total realized is the single cumulative
    figure from earliest apply to now. Card renders "Virtuous cycle: N applies
    → $X projected, $Y realized (Z%)" with weekly breakdown in evidence. New
    types `VirtuousCycleFacts` / `VirtuousCycleWeek` + composer/loader tests.
12. **Phase D Stage 3 item 3 (Exec memo) — DONE** (this commit):
    `internal/orgserver/rollup/virtuous_cycle.go` rollup reads pushed
    `virtuous_cycle` rows from org DB; `internal/orgserver/digest/digest.go`
    `assembleBriefing` fetches virtuous cycle rollup and passes cumulative
    applies/projected/realized to `notifydigest.BriefingData`; email template
    `ComposeBriefing` renders "Virtuous cycle" section with applies, projected,
    realized, percentage, graded turns; `internal/orgserver/ingest/ingest.go`
    upserts pushed `virtuous_cycle` rows into org DB (migration 023);
    `internal/orgserver/virtuous_cycle_handlers.go` exposes
    `GET /api/org/virtuous-cycle?days=` admin endpoint; `internal/store/virtuouscycleorg.go`
    node-side aggregate reads CTA ledger + router_decisions, computes weekly
    trend + cumulative realized (broad filter); push wired via
    `ShareOptions.VirtuousCycle` + `PushBatch.VirtuousCycle` + `PushEnvelope.VirtuousCycle`
    + config `[org_client.share] virtuous_cycle`; `internal/orgcontract/types.go`
    `VirtuousCycleRow` + `PushEnvelope.VirtuousCycle`; config
    `[org_client.share] virtuous_cycle` opt-in.

## Open decisions (spec §6)

1. Day boundary — **DECIDED 2026-08-01: LOCAL machine timezone**; implemented.
2. `analysis_calls` vs reusing `api_turns` for LLM accounting —
   **DECIDED 2026-08-02: separate `analysis_calls` table**; implemented (079 + store seam).
3. Q&A scope: evidence-items-only vs full-session context —
   **DECIDED 2026-08-02: evidence-items-only**; implemented (`internal/llmrun` prompts).
4. Org push of briefing/analysis aggregates: hash-only digest vs exclude —
   **DECIDED 2026-08-02: hash-only digest**; implemented (`briefing_aggregates`
   buckets under the node's own `[org_client.share] briefing_aggregates`
   consent toggle, never raw content; `analysis_calls` remains excluded).
5. LLM narrative trust model: always "generated by <conn>", never new facts — open (documented; template-kept-on-failure already honors it).
6. Which existing LLM callers migrate to the shared client first —
   **DECIDED 2026-08-02: judge → summary** (judgeclient, then intelligence/summary);
   messagesummary + guard-cloud remain on their own clients (follow-up).
7. Management UI storage: runtime `llm_connections` table + `/api/llm/connections`
   CRUD (spec §3.4) vs editing the static TOML block via the settings-section
   mechanism — **DECIDED 2026-08-03: TOML-edit via `PUT /api/config/section/llm`**
   (shipped `f681dab`); one source of truth, reuses the standard section-save
   path (validation + `.bak`). The runtime table, `GET /api/llm/status`, and the
   org web2 page are dropped from Phase B; revisit only if org-side connection
   management is ever required (org server sees content-free aggregates only).
8. Stage 2 "Apply CTA" scope — **DECIDED 2026-08-04: full CTA seam, routing
   lever only** (not the minimal deep-link, not a generic multi-lever
   configapply): a new `internal/configapply` write owner (stamped pre-write
   snapshot + JSONL undo ledger, mirroring `routingapply`), one detector→lever
   mapping (`routing_shadow` → `[routing].policy`), the shadow report carries
   its real decide-under policy name, and plan/apply/revert/ledger endpoints.
   Behavioral detectors (cache_hit_rate, long_context_surcharge,
   trivial_model) deliberately stay lever-less — no honest config write
   realizes their savings. Web card action slot deferred (Stage 2 item 2/3).
9. Stage 2 Routing CTA levers — **DECIDED 2026-08-04: multi-lever plan with
   `change_id` selection** (extends decision 8, supersedes its "one detector→
   lever mapping" for routing_shadow): a **mode** lever (advise→enforce —
   the shadow's reroutes ARE the current policy's rules firing in advise mode,
   so writing them as `[[routing.rules]]` would be a no-op; the honest write
   is to turn enforcement on), a **policy** lever (swap to the shadow's
   decide-under template — only when it's a known template), and a
   **stickiness** lever (lower `min_turns_between_switches` by 1 when
   `stickiness_hold` holds show held savings, priced at the mean reroute
   savings; held decisions carry no per-hold dollar amount, so the heuristic
   is the report's one approximation). Each lever is a first-class
   `configapply.Change` with `ChangeID` + `AlreadyActive`; apply selects by
   `change_id` and re-derives the target value under `configWriteMu`.
   Projected-monthly stays the mode lever's figure (the full shadow savings ×
   30); the policy lever projects 0 by construction (the shadow already runs
   under it). All three bind at daemon start → `restart_required`.

10. Stage 2 Pull rail scope — **DECIDED 2026-08-04: a dedicated read-only
    endpoint, rail = every active card, levered rows reuse the CTA plan
    builder**. The rail is GET /api/briefing/actions (not an extension of
    /api/briefing or the opportunities list) so the compose/recompute cost
    is on-demand and the snapshot (riefing_snapshots) stays pure. Levered
    cards surface ONE row per non-already-active lever (not one row per card)
    so a multi-lever plan renders as distinct, independently-appliable "do
    this" items; guidance-only detectors (no honest config lever) surface as
    ctionable=false rows so the rail covers the whole card set. Already-
    active levers are dropped; a levered card with every lever already active
    falls back to a guidance row (never a no-op offer). UI (web card deck) is
    deferred — API + tests only, consistent with decisions 8/9.
