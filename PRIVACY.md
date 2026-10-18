# Privacy

SuperBased Observer is **a local Go binary that stores what it captures on
your machine**. Your prompts, code and AI responses are not sent anywhere
unless you turn on a feature that sends them. The section
[When observer DOES make a network call](#when-observer-does-make-a-network-call)
is the complete list of network calls the binary makes, including the ones
it makes by default; `observer privacy` prints the same list for your
current configuration. The binary you install from npm is the same binary
published with SLSA provenance to the public GitHub release: what you
install is what runs.

This document spells out exactly what observer touches and what it
never touches.

## TL;DR

- **Local storage.** Observer's database, logs, and stash live under
  `~/.observer/` (per-user, per-platform). Your prompts, code and AI
  responses stay there unless **you** turn on a feature that sends them.
- **What leaves by default.** Two calls, neither carrying session data: a
  daily download of the public model price table from `superbased.app`
  (opt out with `[pricing.feed] enabled = false`), and a TLS warm-up (a
  bare HEAD request) to the default provider hosts when the proxy starts.
  The proxy forwards **your** requests unchanged to the AI provider you
  already use.
- **Everything else is opt-in.** Org enrolment, aggregate sharing, message
  summaries, webhooks, exporters and the rest are listed below with the
  setting that turns each on.
- **No accounts required.** You can run observer offline, behind an
  air-gap, without ever signing in to anything.

## What observer stores locally

| Data | Where | What's in it |
|---|---|---|
| SQLite database | `~/.observer/observer.db` | Normalized session metadata: tool/model/timestamps, token counts, costs, file-path lists, command excerpts, conversation summaries. No raw prompt bodies, no file contents. |
| Operational logs | `~/.observer/logs/` | Start/stop, watcher errors, retention sweeps. No request bodies, no API keys. |
| Stash blobs | `~/.observer/stash/<sha256>` | Content-addressed blobs ONLY when the proxy's stash compressor is on (off by default for Claude Code). These are tool-result bodies your AI client already has copies of. |
| Compression artifacts | DB only | Per-event compression decisions, savings estimates. No content. |

You can inspect everything with standard SQLite tools:

```bash
sqlite3 ~/.observer/observer.db '.schema'
sqlite3 ~/.observer/observer.db 'SELECT * FROM actions LIMIT 5'
```

Delete it all at any time:

```bash
rm -rf ~/.observer/   # full removal, no traces elsewhere on your system
```

## What observer reads

Observer's watcher and hook handler read session logs your AI clients
already write to disk:

- `~/.claude/projects/**` (Claude Code)
- `~/.codex/sessions/**` (Codex)
- `~/.cursor/chats/**` + Cursor's `state.vscdb` (Cursor)
- `~/.copilot/{session-state,logs}/**` (Copilot CLI)
- VS Code's per-extension state for Copilot / Cline / OpenClaw / Pi / etc.
- Antigravity's per-session protobuf records (Google Antigravity)

These files exist whether observer is installed or not. Observer
parses them and writes normalized records into its own SQLite
database; it does not delete, modify, or upload the originals.

## What observer NEVER stores

Per the project's design spec (`superbased-final-spec-v2.md`):

- **File contents.** Only file paths and modification timestamps.
- **Raw command outputs.** Only the command line, exit code, and a
  short scrubbed excerpt.
- **Raw prompt bodies.** Only token counts and a per-message hash.
  Conversation summaries are stored only when message-summary is
  explicitly enabled.
- **Credentials.** The scrubber (`internal/scrub/`) removes API keys,
  bearer tokens, password forms, and known-secret regexes before
  any persistence.

## When observer DOES make a network call

These are the code paths in the binary that touch the network. The first
two run by default and carry no session data; every other one is gated
behind explicit configuration or your own action. `observer privacy`
prints this list, with each row's on/off state, for your configuration.

| Feature | When it talks to the network | What it sends | Opt-in via |
|---|---|---|---|
| **API proxy** | When your AI client points `ANTHROPIC_BASE_URL` / `OPENAI_BASE_URL` at observer | **Your** API request, forwarded byte-identically to the upstream provider you chose (api.anthropic.com, chatgpt.com, openai.com, …) | `observer init --claude-code/--codex/--cursor` (writes the env-var hint into the AI client's config) |
| **Message summary** | If `[messagesummary] enabled = true` | A subset of your conversation, to the LLM endpoint **you** configure (`base_url`, `model`, `api_key`) | TOML config; you provide the endpoint and key |
| **Org enrolment** | If you ran `observer enroll <org-url>` | Aggregate per-day metrics (token totals, action counts, project hashes), and only the fields the org-server explicitly requests | `observer enroll` (explicit, prompts for org URL + token) |
| **Model price table** | Every 24 hours by default (`[pricing.feed]`) | A plain GET of the public, signed price table at `superbased.app/api/pricing/...`; no identifier, session or usage data, only an `If-None-Match` cache tag | On by default; opt out with `[pricing.feed] enabled = false` |
| **TLS warm-up** | At proxy start, when `[proxy].prewarm_targets` is non-empty (the default) | A bare HEAD request to the listed provider hosts so your first real request does not pay the TLS handshake; no session data | On by default; empty `[proxy].prewarm_targets` to turn it off |
| **Usage telemetry** | On by default at the content-free `baseline` tier (`[telemetry] enabled = true`, `tier = "baseline"`); a one-time notice in the daemon output and the dashboard says so before the first batch. Off with `observer telemetry consent off`, `[telemetry] enabled = false` or `OBSERVER_TELEMETRY=off` | Content-free usage events under the published telemetry contract (event names, counts, sizes, timings; no prompt or output text) to the `[telemetry] endpoint` (default `https://cloud.superbased.app`, or a self-hosted reporter after `observer telemetry register`), identified by an install id derived from this machine's cloud device key, with no account | `observer telemetry consent <tier>`; see or delete it with `observer telemetry data` / `observer telemetry delete --yes` |
| **Cloud intelligence** | After `observer cloud login`, for a session you preview (`observer cloud preview`) and consent to (`observer cloud consent`); uploads on `observer cloud sync` | Exactly the bytes the preview showed you, to the hosted cloud service | Sign-in plus per-session consent |
| **Aggregate share** | If `[aggregate_share] enabled = true` and a consent receipt exists | A coarsened monthly usage aggregate to `aggregate.superbased.app` | Config plus consent (`observer aggregate`) |
| **Org update manifest** | When enrolled and `[update] enabled = true` | GETs the signed update manifest (and, when an update applies, the artifact) from your enrolled org server | `observer enroll` |
| **Dashboard update check** | When you click "Check for updates" in Settings | A GET to `registry.npmjs.org` for the latest version number | Your click |
| **`observer summarize`** | When you run it | A session, to the Anthropic Messages API with your own key | Your command |
| **Rolling summarization** | If `[compression.conversation.rolling] enabled = true` and a tool routes through the proxy | Older conversation turns, to the summarizer provider you configure | TOML config |
| **OTel exporter** | If `[exporter.otel] enabled = true` | Turn-level spans to your OTLP collector | TOML config |
| **Email / digest alerts** | If `[email] enabled = true` | Alert or digest mail to your SMTP server | TOML config |
| **Remote notify** | If `[remote.notify] enabled = true` | A webhook / ntfy call on terminal session events | TOML config |
| **Guard cloud** | If `[guard.cloud] enabled = true` and a webhook, judge or reputation lookup is configured | Redacted guard event summaries to your webhooks; an ask-class event's context to your judge endpoint; package metadata GETs from `registry.npmjs.org` | TOML config |
| **Generalized observability (Plane A)** | If `[observability] enabled = true` | Judge calls and alert webhooks for a hosted app you are observing | TOML config |
| **Antigravity gRPC fallback** | When parsing Antigravity sessions on macOS where keychain decrypt fails | Local IPC to a Google-installed gRPC socket, never the internet | Automatic; only fires on Antigravity sessions |

Apart from the two default calls above, observer makes no outbound network
calls until you configure one. Verify with:

```bash
sudo lsof -p $(pgrep -f 'observer start') -i      # should show ONLY :8820/:8830/:8831 etc. on 127.0.0.1
```

## How to audit what leaves your machine

The codebase is open source. To audit:

```bash
# 1. The binary's own report: every outbound path, its gate, and whether it is on
observer privacy

# 2. Watch live sockets: apart from the price table refresh, the TLS warm-up and the proxy's
#    upstream connections, you should see only loopback listeners
sudo lsof -p $(pgrep -f 'observer start') -i

# 3. Run observer with no network at all and confirm everything (parsing, DB, MCP, dashboard) still works
unshare -rn observer start --no-dashboard
```

## What the proxy can see

When you use the proxy, observer's local process **reads** your AI
API requests and responses; it has to, to normalize them and compute
accurate token counts. That data lands in the same local SQLite
database described above. It is **forwarded** to the upstream provider
unchanged (Anthropic / OpenAI / etc.). Observer never sends a copy
anywhere else.

If you don't want the proxy to see request bodies, you can run
observer **without** the proxy:

- Watcher + dashboard + MCP only: `observer start`; observer parses
  your AI client's own session logs (which already contain the same
  data) and skips the proxy entirely.
- Don't run `observer init --claude-code/--codex/--cursor` (or run
  `observer init --skip-proxy-route`); the AI client won't be
  configured to point at the proxy, so requests go direct to the
  provider.

## Org enrolment (opt-in)

Org enrolment (shipped in v1.7.2) lets organizations roll up
**aggregate** metrics from per-user observers into an org-server
dashboard. It is **opt-in** per user via `observer enroll <org-url>`.

What the org-server receives:

- Aggregate counts per day: total tokens, total actions, total cost
  estimate, per-project hashes (NOT paths).
- Capability hashes (which adapters fired), no content.

What it does NOT receive:

- Per-request prompt bodies, file contents, command outputs.
- Identifiable project names (only opaque hashes are sent).
- Anything in real time; payload is a once-per-day rollup.

If you don't want to send aggregate data to your organization's rollup
server, don't enroll. The org-server endpoint is not pre-configured;
there is no default destination.

## Source-of-truth and verification

- Source code: https://github.com/superbasedapp/observer
- npm package: https://www.npmjs.com/package/@superbased/observer
- SLSA provenance: attached as `multiple.intoto.jsonl` to each
  [public GitHub release](https://github.com/superbasedapp/observer/releases),
  verifiable with [`slsa-verifier`](https://github.com/slsa-framework/slsa-verifier)
  (`v2.7.0+` required for private-builder support).
- SBOM: `observer.cdx.json` attached to each release; lists every
  transitive dependency.

## Reporting

If you find behavior that contradicts anything on this page (a
network call we didn't list, a code path that stores something we
said it doesn't), file an issue at
https://github.com/superbasedapp/observer/issues with the
tag `privacy`, or email `contact@superbased.app`.

We treat any unintentional collection as a bug, not a feature.
