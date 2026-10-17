import assert from "node:assert/strict";
import test from "node:test";

import { HOOK_FAILURE_OFF, HOOK_FAILURE_UNKNOWN, guardHookFailureLine } from "./guardHookFailure.ts";

// T-346: the tile states the real per-family hook failure mode, never
// "internal errors never block" on a node whose gates fail closed.
const cases: Array<{ name: string; enabled?: boolean; mode?: string; tool?: string; prompt?: string; want: string }> = [
  { name: "enforcing both families", tool: "closed", prompt: "closed", want: "fail closed: a hook failure blocks tool calls and prompts" },
  { name: "tool family only", tool: "closed", prompt: "open", want: "a hook failure blocks tool calls; prompts fail open" },
  { name: "prompt family only (observe mode, independent prompt enforcement)", mode: "observe", tool: "open", prompt: "closed", want: "a hook failure blocks prompts; tool calls fail open" },
  { name: "nothing enforced", mode: "observe", tool: "open", prompt: "open", want: "fail open: a hook failure never blocks" },
  { name: "guard disabled", enabled: false, tool: "closed", prompt: "closed", want: HOOK_FAILURE_OFF },
  { name: "mode off", mode: "off", want: HOOK_FAILURE_OFF },
  { name: "older daemon: not reported", want: HOOK_FAILURE_UNKNOWN },
];

for (const c of cases) {
  test(c.name, () => {
    const hook_failure = c.tool !== undefined ? { tool_gate: c.tool, prompt_gate: c.prompt ?? "open" } : undefined;
    const line = guardHookFailureLine({ enabled: c.enabled ?? true, mode: c.mode ?? "enforce", hook_failure });
    assert.equal(line, c.want);
    assert.ok(!line.includes("internal errors never block"));
  });
}
