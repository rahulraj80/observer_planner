import { test } from "node:test";
import assert from "node:assert/strict";
import {
  guardToolCallsEnforced,
  guardToolGateChannelLabel,
  guardToolGateLine,
  TOOL_CALLS_NOT_ENFORCED,
  type GuardToolGate,
} from "./guardToolGates.ts";

const enforcing = { enabled: true, mode: "enforce", hook_failure: { tool_gate: "closed" } };

const row = (tool: string, current: boolean, installed = true, channel = current ? "hook_block" : "sandbox_enforce"): GuardToolGate => ({
  tool,
  channel,
  current,
  installed,
  reason: current ? "its PreToolUse hook is registered and current" : "its PreToolUse hook is missing or stale",
});

test("no tool_gates from the daemon: no line", () => {
  assert.equal(guardToolGateLine(undefined, enforcing), null);
});

test("no hook tool installed: no line", () => {
  assert.equal(guardToolGateLine([row("cursor", false, false)], enforcing), null);
});

test("every installed hook tool holds", () => {
  assert.equal(guardToolGateLine([row("claude-code", true), row("codex", true), row("cursor", false, false)], enforcing), "tool calls: all 2 installed hook tools block by their hooks");
  assert.equal(guardToolGateLine([row("claude-code", true)], enforcing), "tool calls: the installed hook tool blocks by its hook");
});

test("a missing or stale hook is never counted as blocking", () => {
  assert.equal(
    guardToolGateLine([row("claude-code", true), row("codex", false)], enforcing),
    "tool calls: 1 of 2 installed hook tools block by their hooks; the rest fall back (see Coverage)",
  );
});

// Review M4-05: a current hook only blocks while the tool family is
// enforced. Guard off says nothing (the hook failure line already does);
// observe without a tool-family override never reads as blocking.
test("the line follows the tool family's enforcement", () => {
  const gates = [row("claude-code", true), row("codex", true)];
  const all = "tool calls: all 2 installed hook tools block by their hooks";
  const cases: Array<{ name: string; sum: Parameters<typeof guardToolGateLine>[1]; want: string | null }> = [
    { name: "guard disabled", sum: { enabled: false, mode: "enforce" }, want: null },
    { name: "mode off", sum: { enabled: true, mode: "off" }, want: null },
    { name: "observe, tool family open", sum: { enabled: true, mode: "observe", hook_failure: { tool_gate: "open" } }, want: TOOL_CALLS_NOT_ENFORCED },
    { name: "observe, older daemon", sum: { enabled: true, mode: "observe" }, want: TOOL_CALLS_NOT_ENFORCED },
    { name: "no summary", sum: undefined, want: TOOL_CALLS_NOT_ENFORCED },
    { name: "observe with a tool-family override", sum: { enabled: true, mode: "observe", hook_failure: { tool_gate: "closed" } }, want: all },
    { name: "enforce, older daemon", sum: { enabled: true, mode: "enforce" }, want: all },
  ];
  for (const c of cases) assert.equal(guardToolGateLine(gates, c.sum), c.want, c.name);
  assert.equal(guardToolCallsEnforced({ enabled: true, mode: "enforce", hook_failure: { tool_gate: "open" } }), false);
});

test("channel labels", () => {
  assert.equal(guardToolGateChannelLabel("hook_block"), "blocked by its hook");
  assert.equal(guardToolGateChannelLabel("sandbox_enforce"), "sandboxed launch");
  assert.equal(guardToolGateChannelLabel("recorded_acceptance"), "recorded only");
  assert.equal(guardToolGateChannelLabel("new_channel"), "new_channel");
});
