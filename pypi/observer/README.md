# superbased-observer

<img src="https://raw.githubusercontent.com/superbasedapp/observer/main/vscode/media/logo-dark.png" alt="SuperBased logo" height="56">

**Local-first observability for AI coding agents.** See what Claude Code, Codex, Cursor and every
other coding agent you run really cost, from the tokens your provider actually billed. On your own
machine, with no account.

[![PyPI](https://img.shields.io/pypi/v/superbased-observer.svg)](https://pypi.org/project/superbased-observer/)
[![Python: 3.8+](https://img.shields.io/pypi/pyversions/superbased-observer.svg)](https://pypi.org/project/superbased-observer/)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](https://github.com/superbasedapp/observer/blob/main/LICENSE)
[![Website](https://img.shields.io/badge/website-superbased.app-2EC4B6.svg)](https://superbased.app/)

![SuperBased dashboard Overview: sessions, cost over time by billing bucket, top models and coding tools](https://raw.githubusercontent.com/superbasedapp/observer/main/docs/assets/screenshots/01-overview.png)

## Try it without installing

```bash
uvx --from superbased-observer observer usage
```

Reads the session logs your coding agents already keep on disk, prints a 30-day cost table by tool
and model, and deletes its temporary database on exit. No config, no network. (The package is
`superbased-observer`; the command is `observer`.)

## Install

```bash
uv tool install superbased-observer    # or: pipx install superbased-observer
observer --version
```

Each wheel bundles its platform's prebuilt binary (manylinux2014 x86_64 and aarch64, macOS Intel
and Apple Silicon, win_amd64): no compile step, no download at install time, no Go toolchain. The
same binary ships on [npm](https://www.npmjs.com/package/@superbased/observer) and the
[VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=superbased.superbased-observer)
with the same version numbers; install from one channel only.

## Why SuperBased

**One local binary that tracks Claude Code token usage, Codex cost and Cursor spend side by side,
and helps you control them.**

- **Provider-exact cost, not estimates.** An optional local proxy records the tokens your provider
  returned: net input, 5-minute vs 1-hour cache writes, reasoning tokens and long-context repricing.
- **Every coding agent in one place.** 40 adapters normalize Claude Code, Codex, Cursor, GitHub
  Copilot, Gemini CLI, Cline, Aider, Kiro, Devin, OpenCode, Junie and more into one local database.
- **Local-first, no account.** No telemetry and no sign-in. Everything lives in one SQLite file
  under `~/.observer/`.
- **Know the next bill before it lands.** Prompt-cache tracking with expiry warnings, a cost band
  for your next message, and a 5-hour and weekly limit gauge for proxied sessions.
- **Guardrails on your own agents.** Built-in and custom rules for destructive commands, secrets
  egress, MCP pinning and budgets. Observe mode by default; `observer guard simulate` first.
- **Your agents as terminals.** Launch agent CLIs from the dashboard, join a running session from a
  paired device over your Tailscale tailnet, and hand a session from one tool to another.

## Get started

```bash
observer start    # proxy + watcher + dashboard; opens http://localhost:8081
observer scan     # backfill history from the session logs already on disk
observer doctor   # check the install
```

For provider-exact tokens, route your agent through the proxy: in the dashboard open Compression,
click your tool's status pill and choose **Route through the observer proxy** (it previews the
config change and writes only when you confirm), or run `observer claude` / `observer codex` for a
one-off proxied session. `observer init` additionally registers the opt-in MCP server.

## What you get

- A local dashboard for sessions, cost, cache, security and live activity, filterable by tool and
  project.
- Cost per session, model, project and tool, with cache read/write splits down to each API turn.
- An opt-in MCP server so your agents can query their own history across tools.
- Waste detection: stale re-reads, repeated commands and files several agents fight over.
- Dashboard terminals with multi-seat join and native resume.
- A CLI for everything: `observer usage`, `cost`, `predict`, `tail`, `statusline`.

## Privacy

No telemetry and no account. SuperBased stores paths, commands and short excerpts, scrubs secrets
before writing, and binds the proxy and dashboard to `127.0.0.1` by default. The proxy forwards your
own requests, unchanged, to the provider you already use. See
[PRIVACY.md](https://github.com/superbasedapp/observer/blob/main/PRIVACY.md).

## Learn more

- [Documentation](https://superbased.app/docs) and [quickstart](https://superbased.app/docs/getting-started/quickstart)
- [Supported agents and per-tool setup](https://superbased.app/docs/track/overview)
- [How SuperBased compares](https://superbased.app/docs/compare/overview)
- [Source, issues and changelog](https://github.com/superbasedapp/observer)

## Troubleshooting

- **`externally-managed-environment` from pip:** your distro protects the system Python (PEP 668).
  Use `uv tool install` or `pipx install`, which give the tool its own environment.
- **`observer: command not found`:** make sure the installer's `bin` directory (usually
  `~/.local/bin`) is on `PATH`; `pip show -f superbased-observer` lists the exact path.
- **Port 8820 or 8081 already in use:** another `observer start` is running; stop it first.

## License

Apache 2.0. This package is a small Python launcher that runs the prebuilt binary bundled in the
wheel.
