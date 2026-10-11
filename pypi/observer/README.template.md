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

<!-- @@INCLUDE:docs/distribution/README-body.md@@ -->

## Troubleshooting

- **`externally-managed-environment` from pip:** your distro protects the system Python (PEP 668).
  Use `uv tool install` or `pipx install`, which give the tool its own environment.
- **`observer: command not found`:** make sure the installer's `bin` directory (usually
  `~/.local/bin`) is on `PATH`; `pip show -f superbased-observer` lists the exact path.
- **Port 8820 or 8081 already in use:** another `observer start` is running; stop it first.

## License

Apache 2.0. This package is a small Python launcher that runs the prebuilt binary bundled in the
wheel.
