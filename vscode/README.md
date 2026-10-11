<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/superbasedapp/observer/main/vscode/media/logo-light.png">
    <img src="https://raw.githubusercontent.com/superbasedapp/observer/main/vscode/media/logo-dark.png" alt="SuperBased logo" height="72">
  </picture>
</p>

<h1 align="center">SuperBased for VS Code</h1>

<p align="center">
  <strong>Local-first observability for AI coding agents, inside your editor.</strong><br>
  See what Claude Code, Codex, Cursor and every other coding agent you run really cost, from the
  tokens your provider actually billed. No account, no telemetry.
</p>

<p align="center">
  <img src="https://raw.githubusercontent.com/superbasedapp/observer/main/docs/assets/screenshots/01-overview.png" alt="SuperBased dashboard Overview: sessions, cost over time by billing bucket, top models and coding tools" width="860">
</p>

## Why SuperBased

- **Provider-exact cost.** A local proxy records the tokens your provider returned, including
  5-minute vs 1-hour cache writes, reasoning tokens and long-context repricing.
- **Every coding agent in one place.** 40 adapters: Claude Code, Codex, Cursor, GitHub Copilot,
  Gemini CLI, Cline, Aider, Kiro, Devin, OpenCode, Junie and more, in one local database.
- **Local-first.** Everything stays in `~/.observer/observer.db` on your workspace host.
- **Know the next bill before it lands.** Prompt-cache expiry warnings and a cost band for your next
  message.

## Get started

1. **Install the extension.** Each platform build bundles the `observer` binary, so there is nothing
   else to install.
2. **Nothing to start.** In the default `auto` mode the extension attaches to a running daemon or
   starts one, and only ever stops a daemon it started.
3. **Open the dashboard:** run **SuperBased: Open Dashboard**, or click the spend item in the status
   bar.
4. **Capture a session with exact tokens:** open a terminal with the **AI Coding Tool
   (SuperBased-proxied)** profile (terminal panel, the arrow next to `+`), then run `claude`,
   `codex` or another agent CLI. Or use **SuperBased: Launch Claude Code (proxied)** / **Launch Codex
   (proxied)**.

If something looks off, run **SuperBased: Doctor**. The **Get Started with SuperBased** walkthrough
(Help > Welcome) covers the rest.

## What you get

| Surface | What it does |
|---|---|
| **Status bar** | Today's spend at a glance, plus a prompt-cache status item. Click to open the dashboard. |
| **Sidebar** | Today, Sessions, Discovery and Costs (7d) trees in the SuperBased activity bar view. |
| **Dashboard tab** | The full local dashboard (sessions, cost, cache, security, live activity) in an editor tab. |
| **File decorations** | A marker on files your agents touched recently, with who read or edited them. |
| **Terminal profile** | Pre-sets the proxy environment so agent CLIs started from it route through SuperBased. |
| **Instruction CodeLens** | Refresh or preview suggestions for your agent instruction files from captured patterns. |

<p align="center">
  <img src="https://raw.githubusercontent.com/superbasedapp/observer/main/docs/assets/screenshots/11-session-detail.png" alt="Session detail: cost, token buckets by billing tier, next-message cost predictor and rate-limit gauge" width="860">
</p>

## Settings

| Setting | Default | Purpose |
|---|---|---|
| `observer.daemon.mode` | `auto` | `auto` attaches or starts a daemon; `detect` only attaches. |
| `observer.binary.path` | empty | Use a specific `observer` binary instead of the bundled one. |
| `observer.binary.preferPathBinary` | `false` | Prefer an `observer` already on your `PATH`. |
| `observer.dashboard.port` | `8081` | Dashboard port. |
| `observer.proxy.port` | `8820` | Proxy port. |
| `observer.statusBar.enabled` | `true` | Show today's spend in the status bar. |
| `observer.cacheStatusBar.enabled` | `true` | Show the prompt-cache status item. |

## Privacy

No telemetry and no account. After the binary is in place, the extension talks only to
`127.0.0.1` on your workspace host; it contacts GitHub Releases only if it finds neither a bundled
nor a local binary. Remote-SSH, WSL, Dev Containers and Codespaces keep the data on the remote host.

## Learn more

- [User guide](https://github.com/superbasedapp/observer/blob/main/docs/vscode-extension-user-guide.md)
  and [command and settings reference](https://github.com/superbasedapp/observer/blob/main/docs/vscode-extension.md)
- [SuperBased docs](https://superbased.app/docs) and [source on GitHub](https://github.com/superbasedapp/observer)
- [Issues](https://github.com/superbasedapp/observer/issues) and
  [Discussions](https://github.com/superbasedapp/observer/discussions). The **SuperBased** output
  channel (View > Output) shows what the extension is doing; include it in bug reports.

<p align="center"><sub>Apache 2.0. Also available on <a href="https://open-vsx.org/extension/superbased/superbased-observer">Open VSX</a> for Cursor, VSCodium and Windsurf.</sub></p>
