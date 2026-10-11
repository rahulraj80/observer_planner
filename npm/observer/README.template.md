# @superbased/observer

<img src="https://raw.githubusercontent.com/superbasedapp/observer/main/vscode/media/logo-dark.png" alt="SuperBased logo" height="56">

**Local-first observability for AI coding agents.** See what Claude Code, Codex, Cursor and every
other coding agent you run really cost, from the tokens your provider actually billed. On your own
machine, with no account.

[![npm](https://img.shields.io/npm/v/@superbased/observer.svg)](https://www.npmjs.com/package/@superbased/observer)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](https://github.com/superbasedapp/observer/blob/main/LICENSE)
[![Website](https://img.shields.io/badge/website-superbased.app-2EC4B6.svg)](https://superbased.app/)

![SuperBased dashboard Overview: sessions, cost over time by billing bucket, top models and coding tools](https://raw.githubusercontent.com/superbasedapp/observer/main/docs/assets/screenshots/01-overview.png)

## Try it without installing

```bash
npx @superbased/observer
```

Reads the session logs your coding agents already keep on disk, prints a 30-day cost table by tool
and model, and deletes its temporary database on exit. No config, no network.

## Install

```bash
npm install -g @superbased/observer
observer --version
```

Prebuilt binaries for Linux (x64, arm64), macOS (Intel, Apple Silicon) and Windows x64. npm fetches
only the one for your machine; there is no compile step and no postinstall download. Also on
[PyPI](https://pypi.org/project/superbased-observer/) and the
[VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=superbased.superbased-observer)
with the same version numbers; install from one channel only.

<!-- @@INCLUDE:docs/distribution/README-body.md@@ -->

## Troubleshooting

- **`EACCES` on `npm install -g`:** point npm at a user prefix
  (`npm config set prefix ~/.npm-global` and add `~/.npm-global/bin` to `PATH`) or use a Node version
  manager such as fnm or nvm.
- **`observer: command not found`:** make sure your npm global `bin` directory is on `PATH`, and that
  you installed `@superbased/observer` itself, not only a platform package.
- **Port 8820 or 8081 already in use:** another `observer start` is running; stop it first.

## License

Apache 2.0. This package is a small Node.js shim that runs the prebuilt binary from the matching
`@superbased/observer-<platform>-<arch>` package.
