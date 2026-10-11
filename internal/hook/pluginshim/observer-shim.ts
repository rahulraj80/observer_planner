// SuperBased Observer plugin shim: one file for every in-process plugin host.
//
// Rendered per host by internal/hook/pluginshim (go:embed): the PROFILE
// literal below is the only part that differs between hosts. It names the
// host's plugin contract ("shape"), the native events this install
// subscribes to (each mapped to the abstract hook capability it provides),
// and the deadlines. Every subscribed event is forwarded as JSON on stdin to
// `observer hook <tool> <event>`; the reply ({"decision": ...}) is mapped
// onto the host's own contract (throw, a returned object, a mutated output).
//
// Written as plain JavaScript (no type annotations) so the same bytes load
// under Bun (OpenCode, Kilo CLI), jiti (pi, commandcode), Node (OpenClaw)
// and Cordis (DeepSeek Harness) without a compile step, and so the tests can
// run it under plain Node.
//
// Failure semantics (hook-parity audit section 5.7):
//   - observer answers inside the deadline: its decision stands. observer
//     applies the fail-closed rule itself (its watchdog runs below this
//     shim's deadline, `--max-runtime`), so a slow evaluation under
//     enforcement already comes back as a deny with a reason.
//   - observer does not answer at all (binary missing, hung, killed at this
//     shim's own deadline, unparseable output): a GATE event is denied with
//     a reason when guard enforcement is in force, and allowed otherwise.
//     "In force" is read only to TIGHTEN, never to open: a recent observer
//     reply's `enforced: true` hint, or the daemon's guard-posture.json
//     while it is FRESH (written within the posture window, not dated in the
//     future, not older than config.toml: the T-050 rule the Go hook
//     applies). A stale posture is ignored, so a stopped daemon cannot keep
//     failures closed forever, and an `enforced: false` hint never opens a
//     failure the posture would close. Either way the failure is appended to
//     guard-hookfail.jsonl for the daemon with its outcome (fail_closed or
//     fail_open, T-049). Observe events never wait and never block.
//   - nothing at module load can throw: the host keeps working when this
//     shim cannot run.

import { spawn, spawnSync } from "node:child_process";
import { readFileSync, appendFileSync, statSync } from "node:fs";

const PROFILE = {{OBSERVER_SHIM_PROFILE}};

const FAIL_TIMEOUT =
  "Observer could not evaluate this call in time; it is blocked because guard enforcement is on. Retry, or ask your admin if this repeats.";
const FAIL_ERROR =
  "Observer could not evaluate this call because of an internal error; it is blocked because guard enforcement is on. Retry, or ask your admin if this repeats.";
const BLOCKED = "Blocked by the SuperBased Observer guard.";
const MAX_OUT = 1 << 20;
const HINT_TTL_MS = 10 * 60 * 1000;
const FIELD_CAP = 256;

const BIN = process.env.OBSERVER_BIN || PROFILE.bin || "observer";
const CONFIG = process.env.OBSERVER_CONFIG || PROFILE.config || "";

// enforced hint per capability, from the most recent observer reply.
const hints = {};

function has(event) {
  return Object.prototype.hasOwnProperty.call(PROFILE.events, event);
}

function capOf(event) {
  return PROFILE.events[event] || "";
}

function str(v) {
  return typeof v === "string" ? v : "";
}

function pick(obj, keys) {
  if (!obj || typeof obj !== "object") return undefined;
  for (const k of keys) {
    if (obj[k] !== undefined && obj[k] !== null && obj[k] !== "") return obj[k];
  }
  return undefined;
}

function argvFor(event, deadlineMs) {
  const args = ["hook", PROFILE.tool, event];
  if (deadlineMs > 0) args.push("--max-runtime", Math.max(300, deadlineMs - PROFILE.margin_ms) + "ms");
  if (CONFIG) args.push("--config", CONFIG);
  if (process.platform === "win32" && PROFILE.wsl_bin) {
    return { bin: "wsl.exe", args: ["-d", PROFILE.wsl_distro, "--", PROFILE.wsl_bin, ...args] };
  }
  return { bin: BIN, args };
}

function parseReply(out) {
  const lines = String(out || "").split(/\r?\n/);
  for (let i = lines.length - 1; i >= 0; i--) {
    const line = lines[i].trim();
    if (!line) continue;
    try {
      const v = JSON.parse(line);
      if (v && typeof v === "object" && !Array.isArray(v)) return v;
    } catch (_e) {
      // not JSON: keep looking upwards
    }
  }
  return null;
}

function run(event, payload, deadlineMs) {
  return new Promise((resolve) => {
    let done = false;
    let out = "";
    let timer = null;
    const finish = (r) => {
      if (done) return;
      done = true;
      if (timer) clearTimeout(timer);
      resolve(r);
    };
    let child;
    try {
      const { bin, args } = argvFor(event, deadlineMs);
      child = spawn(bin, args, { stdio: ["pipe", "pipe", "ignore"], windowsHide: true });
    } catch (_e) {
      finish({ kind: "crash" });
      return;
    }
    timer = setTimeout(() => {
      try {
        child.kill("SIGKILL");
      } catch (_e) {
        // already gone
      }
      finish({ kind: "timeout" });
    }, deadlineMs);
    child.on("error", () => finish({ kind: "crash" }));
    if (child.stdout) {
      child.stdout.on("data", (d) => {
        if (out.length < MAX_OUT) out += d;
      });
    }
    child.on("close", () => {
      const reply = parseReply(out);
      finish(reply ? { reply } : { kind: "unparseable" });
    });
    if (child.stdin) {
      child.stdin.on("error", () => {});
      try {
        child.stdin.end(JSON.stringify(payload));
      } catch (_e) {
        // the close handler reports what happened
      }
    }
  });
}

function runSync(event, payload, deadlineMs) {
  try {
    const { bin, args } = argvFor(event, deadlineMs);
    const r = spawnSync(bin, args, {
      input: JSON.stringify(payload),
      timeout: deadlineMs,
      killSignal: "SIGKILL",
      encoding: "utf8",
      maxBuffer: MAX_OUT,
      windowsHide: true,
    });
    if (r.error) return { kind: r.error.code === "ETIMEDOUT" ? "timeout" : "crash" };
    if (r.signal) return { kind: "timeout" };
    const reply = parseReply(r.stdout);
    return reply ? { reply } : { kind: "unparseable" };
  } catch (_e) {
    return { kind: "crash" };
  }
}

// postureFresh applies the T-050 freshness rule to a posture: written within
// PROFILE.posture_max_age_ms, not more than PROFILE.posture_skew_ms in the
// future, and not older than the config file it was derived from.
function postureFresh(p) {
  // RFC 3339 with up to nanosecond fractions: keep milliseconds for Date.
  const at = Date.parse(str(p.written_at).replace(/(\.\d{3})\d+/, "$1"));
  if (!Number.isFinite(at)) return false;
  const now = Date.now();
  if (now - at > PROFILE.posture_max_age_ms || at - now > PROFILE.posture_skew_ms) return false;
  if (PROFILE.config_file) {
    try {
      if (statSync(PROFILE.config_file).mtimeMs > at) return false;
    } catch (_e) {
      // no config file: nothing changed after the posture was written
    }
  }
  return true;
}

function postureClosed(cap) {
  if (!PROFILE.posture) return false;
  try {
    const p = JSON.parse(readFileSync(PROFILE.posture, "utf8"));
    return (
      !!p &&
      p.schema === PROFILE.posture_schema &&
      p.enforced === true &&
      Array.isArray(p.gate_caps) &&
      p.gate_caps.includes(cap) &&
      postureFresh(p)
    );
  } catch (_e) {
    return false;
  }
}

// failsClosed only ever TIGHTENS: a recent enforced:true hint closes, and so
// does a fresh posture; an enforced:false hint opens nothing.
function failsClosed(cap) {
  const h = hints[cap];
  if (h && h.enforced && Date.now() - h.at < HINT_TTL_MS) return true;
  return postureClosed(cap);
}

function isGate(cap) {
  return cap === "tool_gate" || cap === "prompt_gate";
}

function clip(s) {
  s = str(s);
  return s.length > FIELD_CAP ? s.slice(0, FIELD_CAP) : s;
}

function spool(event, cap, sessionId, kind, outcome) {
  if (!PROFILE.spool) return;
  try {
    const line = JSON.stringify({
      tool: clip(PROFILE.tool),
      event: clip(event),
      cap: clip(cap),
      session_id: clip(sessionId),
      kind: clip(kind),
      at: new Date().toISOString(),
      outcome,
    });
    appendFileSync(PROFILE.spool, line + "\n", { mode: 0o600 });
  } catch (_e) {
    // best effort: the answer already went out
  }
}

function verdictOf(reply) {
  const d = str(reply.decision).toLowerCase();
  if (d === "deny" || d === "block") return { decision: "deny", reason: str(reply.reason) || BLOCKED };
  if (d === "ask") return { decision: "ask", reason: str(reply.reason) || BLOCKED };
  if (d === "modify") return { decision: "modify", prompt: reply.prompt, reason: str(reply.reason) };
  return { decision: "allow", message: str(reply.message) };
}

// failed answers a gate event observer could not: closed under enforcement,
// open otherwise, and recorded either way (T-049: a fail-open failure is a
// guard event too).
function failed(event, cap, sessionId, kind) {
  if (!isGate(cap)) return { decision: "allow" };
  if (failsClosed(cap)) {
    spool(event, cap, sessionId, kind, "fail_closed");
    return { decision: "deny", reason: kind === "timeout" ? FAIL_TIMEOUT : FAIL_ERROR };
  }
  spool(event, cap, sessionId, kind, "fail_open");
  return { decision: "allow" };
}

function settle(event, cap, sessionId, r) {
  if (r.reply) {
    if (typeof r.reply.enforced === "boolean") hints[cap] = { enforced: r.reply.enforced, at: Date.now() };
    return verdictOf(r.reply);
  }
  return failed(event, cap, sessionId, r.kind);
}

async function gate(event, payload) {
  const cap = capOf(event);
  try {
    return settle(event, cap, payload.session_id, await run(event, payload, PROFILE.gate_ms));
  } catch (_e) {
    return failed(event, cap, payload.session_id, "crash");
  }
}

function gateSync(event, payload) {
  const cap = capOf(event);
  try {
    return settle(event, cap, payload.session_id, runSync(event, payload, PROFILE.gate_ms));
  } catch (_e) {
    return failed(event, cap, payload.session_id, "crash");
  }
}

function observe(event, payload) {
  try {
    const { bin, args } = argvFor(event, PROFILE.observe_ms);
    const child = spawn(bin, args, { stdio: ["pipe", "ignore", "ignore"], windowsHide: true });
    child.on("error", () => {});
    const timer = setTimeout(() => {
      try {
        child.kill("SIGKILL");
      } catch (_e) {
        // already gone
      }
    }, PROFILE.observe_ms);
    if (timer.unref) timer.unref();
    child.on("close", () => clearTimeout(timer));
    if (child.unref) child.unref();
    if (child.stdin) {
      child.stdin.on("error", () => {});
      child.stdin.end(JSON.stringify(payload));
    }
  } catch (_e) {
    // observe events never affect the host
  }
}

function envelope(event, sessionId, cwd, extra) {
  return Object.assign(
    { host: PROFILE.host, event, session_id: str(sessionId), cwd: str(cwd) || process.cwd(), pid: process.pid },
    extra,
  );
}

function blocks(v) {
  return v.decision === "deny" || v.decision === "ask";
}

// --- OpenCode and Kilo CLI: {id, server} plugin module ----------------------

// toast tells the DEVELOPER what happened to a prompt (the model only sees
// the replaced text): the host's own TUI toast through the plugin input's
// SDK client, client.tui.showToast({body: {title, message, variant,
// duration}}) (POST /tui/show-toast). Grounded in @opencode-ai/sdk 1.18.31
// (the plugin's v1 createOpencodeClient) and the Kilo CLI 7.8.3 binary,
// which carries the same call. Fire-and-forget: never awaited, never throws,
// a missing client or a headless run (`opencode run`, no TUI) shows nothing.
function toast(client, message, variant, duration) {
  try {
    const tui = client && client.tui;
    if (!tui || typeof tui.showToast !== "function") return;
    const p = tui.showToast({ body: { title: "SuperBased Observer", message, variant, duration } });
    if (p && typeof p.catch === "function") p.catch(() => {});
  } catch (_) {
    // a broken client never breaks the hook
  }
}

function opencodePlugin() {
  const gateEvents = { "tool.execute.before": true, "chat.message": true };
  return {
    id: "superbased-observer",
    server: async (input) => {
      const cwd = (input && (input.directory || input.worktree)) || process.cwd();
      const client = input && input.client;
      const hooks = {};
      if (has("tool.execute.before")) {
        hooks["tool.execute.before"] = async (i, o) => {
          const v = await gate(
            "tool.execute.before",
            envelope("tool.execute.before", i && i.sessionID, cwd, {
              tool_name: str(i && i.tool),
              tool_input: (o && o.args) || {},
              call_id: str(i && i.callID),
            }),
          );
          if (blocks(v)) throw new Error(v.reason);
        };
      }
      if (has("chat.message")) {
        hooks["chat.message"] = async (i, o) => {
          const parts = o && Array.isArray(o.parts) ? o.parts : [];
          const texts = parts.filter((p) => p && p.type === "text" && typeof p.text === "string");
          if (!texts.length) return;
          const v = await gate(
            "chat.message",
            envelope("chat.message", i && i.sessionID, cwd, { prompt: texts.map((p) => p.text).join("\n") }),
          );
          let replacement = null;
          if (blocks(v)) {
            replacement = "[SuperBased Observer withheld this message from the model] " + v.reason;
            toast(client, "Your message was withheld from the model: " + v.reason, "warning", 8000);
          } else if (v.decision === "modify" && typeof v.prompt === "string") {
            replacement = v.prompt;
            toast(client, "Your message was changed before it reached the model.", "info", 5000);
          }
          if (replacement === null) return;
          texts[0].text = replacement;
          for (let k = parts.length - 1; k >= 0; k--) {
            if (parts[k] !== texts[0] && texts.includes(parts[k])) parts.splice(k, 1);
          }
        };
      }
      const bus = Object.keys(PROFILE.events).filter((e) => !gateEvents[e] && !e.startsWith("experimental."));
      if (bus.length) {
        hooks.event = async (arg) => {
          const ev = arg && arg.event;
          if (!ev || !bus.includes(ev.type)) return;
          const props = ev.properties || {};
          const sid = pick(props, ["sessionID", "sessionId"]) || pick(props.info || {}, ["id"]);
          observe(ev.type, envelope(ev.type, sid, cwd, {}));
        };
      }
      return hooks;
    },
  };
}

// --- pi: default-exported extension factory ---------------------------------

function piSession(ctx) {
  try {
    const sm = ctx && ctx.sessionManager;
    if (!sm) return "";
    if (typeof sm.getSessionId === "function") return str(sm.getSessionId());
    return str(sm.sessionId);
  } catch (_e) {
    return "";
  }
}

function piExtension() {
  return function (pi) {
    if (!has("tool_call")) return;
    pi.on("tool_call", async (event, ctx) => {
      const v = await gate(
        "tool_call",
        envelope("tool_call", piSession(ctx), ctx && ctx.cwd, {
          tool_name: str(event && event.toolName),
          tool_input: (event && event.input) || {},
          call_id: str(event && event.toolCallId),
        }),
      );
      if (blocks(v)) return { block: true, reason: v.reason };
      return undefined;
    });
  };
}

// --- OpenClaw: plugin entry with register(api) -------------------------------

function openclawPlugin() {
  return {
    id: "superbased-observer",
    name: "SuperBased Observer",
    description: "Observer guard tool gate.",
    register(api) {
      if (!has("before_tool_call")) return;
      api.on(
        "before_tool_call",
        async (event, ctx) => {
          const v = await gate(
            "before_tool_call",
            envelope("before_tool_call", pick(ctx, ["sessionId", "sessionKey"]), "", {
              tool_name: str(event && event.toolName),
              tool_input: (event && event.params) || {},
              call_id: str(event && event.toolCallId),
            }),
          );
          if (v.decision === "ask" && PROFILE.ask) {
            return { requireApproval: { title: "SuperBased Observer guard", description: v.reason, severity: "warning" } };
          }
          if (blocks(v)) return { block: true, blockReason: v.reason };
          return undefined;
        },
        { timeoutMs: PROFILE.gate_ms + PROFILE.margin_ms },
      );
    },
  };
}

// --- DeepSeek Harness: Cordis plugin ------------------------------------------

function cordisPlugin() {
  return {
    name: "superbased-observer",
    apply(ctx) {
      if (!has("tools/pre-execute")) return;
      ctx.on("tools/pre-execute", async (exec, next) => {
        const tool = pick(exec, ["name", "toolName"]) || pick((exec && exec.tool) || {}, ["name"]);
        const v = await gate(
          "tools/pre-execute",
          envelope("tools/pre-execute", pick(exec, ["sessionId", "session_id"]), pick(exec, ["cwd"]), {
            tool_name: str(tool),
            tool_input: pick(exec, ["input", "args", "arguments", "params"]) || {},
            call_id: str(pick(exec, ["id", "callId", "toolCallId"])),
          }),
        );
        if (blocks(v)) return { kind: "deny", reason: v.reason };
        return next();
      });
    },
  };
}

// --- commandcode: Mods SDK default-exported factory ---------------------------

function commandcodeMod() {
  return function (cmd) {
    const hooks = {};
    if (has("transformInput")) {
      // Synchronous, and the payload is exactly {text}: the contract the
      // command-code prompt dialect (internal/hook/promptsubmit.go) reads.
      hooks.transformInput = ({ text }) => {
        const cap = capOf("transformInput");
        const r = runSync("transformInput", { text }, PROFILE.gate_ms);
        if (r.reply) {
          if (r.reply.action === "handled") return { action: "handled", message: r.reply.message };
          if (r.reply.action === "transform" && typeof r.reply.text === "string") {
            return { action: "transform", text: r.reply.text };
          }
          return { action: "continue" };
        }
        const v = failed("transformInput", cap, "", r.kind);
        return blocks(v) ? { action: "handled", message: v.reason } : { action: "continue" };
      };
    }
    if (has("beforeToolCall")) {
      hooks.beforeToolCall = async (arg) => {
        const v = await gate(
          "beforeToolCall",
          envelope("beforeToolCall", "", "", {
            tool_name: str(arg && arg.toolName),
            tool_input: (arg && arg.input) || {},
            call_id: str(arg && arg.toolCallId),
          }),
        );
        if (blocks(v)) return { block: true, additionalContext: v.reason };
        return undefined;
      };
    }
    cmd.hooks(hooks);
  };
}

const SHAPES = {
  opencode: opencodePlugin,
  pi: piExtension,
  openclaw: openclawPlugin,
  cordis: cordisPlugin,
  commandcode: commandcodeMod,
};

function entry() {
  try {
    const make = SHAPES[PROFILE.shape];
    if (make) return make();
  } catch (_e) {
    // fall through to the inert entry
  }
  return { id: "superbased-observer", server: async () => ({}) };
}

// Exposed for the shim's own tests only (a property on the default export,
// never a second named export: OpenCode's legacy loader rejects a module
// with a non-function export).
const ENTRY = entry();
try {
  Object.defineProperty(ENTRY, "__observerShim", { value: { gate, gateSync, PROFILE }, enumerable: false });
} catch (_e) {
  // a frozen entry simply has no test handle
}

export default ENTRY;
