# docs/plans — index

Single entry point for baseline telemetry implementation planning. Each sub-plan is a separate file under this folder.

## Core plan

- `docs/plans/baseline-telemetry-implementation-plan.md` — master plan: objectives, locked decisions, current state, build order, combined detailed plan, verification, out-of-scope. Read this first.

## Sub-plans (created as implementation progresses)

- `docs/plans/contract-and-tier-model.md` — shared contract + tier model + baseline event + no-text-fields test + CI drift gate (Step 5.1 — implemented and verified: contract v1.1.0 landed in reporterd + enterprise + individual repos).
- `docs/plans/reporterd-install-key-and-self-view.md` — reporterd install-key model + individual API-only self view + delete/export by install key.
- `docs/plans/reporterd-tls.md` — TLS on sup for `https://superbaseddev.bot.nu:48089`.
- `docs/plans/reporterd-self-host.md` — self-host distribution story + runbook.
- `docs/plans/reporterd-tenant-dashboard.md` — enterprise tenant web dashboard + tenant API.
- `docs/plans/system-nudge-classifier.md` — local system-nudge classifier bootstrap + improvement loop.
- `docs/plans/enterprise-baseline-rail.md` — enterprise repo baseline telemetry rail default-on.
- `docs/plans/individual-baseline-rail.md` — individual repo baseline telemetry rail default-on.
- `docs/plans/giveaways-partner-providers.md` — giveaways / partner API provider program.
- `docs/plans/privacy-disclosure-rewrite.md` — remove "No telemetry" claims + rewrite privacy text.

## Conventions

- Plans live under `docs/plans/`, not the repo root.
- Each plan names its verification explicitly (what "done" means).
- Each plan names its open items and out-of-scope explicitly.
- If a plan touches a surface that exists in both repos, it names which repo(s) it changes.

