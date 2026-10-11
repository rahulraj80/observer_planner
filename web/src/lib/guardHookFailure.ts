// The Security page Guard posture tile's second line (T-346): what a
// FAILING gate hook does on this node, per gate family. A hook that times
// out, cannot load its config or cannot parse the payload BLOCKS the call
// when its family is enforced, and lets it run unevaluated otherwise
// (D-194). [guard].strict does not decide it, so the line is never derived
// from strict. The daemon reports the mode as /api/guard/summary's
// hook_failure; an older daemon omits it, and then the line says so rather
// than guessing.

export type GuardHookFailure = {
  tool_gate: "closed" | "open" | string;
  prompt_gate: "closed" | "open" | string;
};

export type GuardPostureInput = {
  enabled: boolean;
  mode: string;
  hook_failure?: GuardHookFailure;
};

// HOOK_FAILURE_COPY is the closed table: [tool closed, prompt closed] -> line.
const HOOK_FAILURE_COPY: ReadonlyArray<{ tool: boolean; prompt: boolean; line: string }> = [
  { tool: true, prompt: true, line: "fail closed: a hook failure blocks tool calls and prompts" },
  { tool: true, prompt: false, line: "a hook failure blocks tool calls; prompts fail open" },
  { tool: false, prompt: true, line: "a hook failure blocks prompts; tool calls fail open" },
  { tool: false, prompt: false, line: "fail open: a hook failure never blocks" },
];

export const HOOK_FAILURE_OFF = "guard off: hooks never block";
export const HOOK_FAILURE_UNKNOWN = "hook failure mode not reported by this daemon";

export function guardHookFailureLine(sum: GuardPostureInput): string {
  if (!sum.enabled || sum.mode === "off") return HOOK_FAILURE_OFF;
  const hf = sum.hook_failure;
  if (!hf) return HOOK_FAILURE_UNKNOWN;
  const tool = hf.tool_gate === "closed";
  const prompt = hf.prompt_gate === "closed";
  return HOOK_FAILURE_COPY.find((r) => r.tool === tool && r.prompt === prompt)?.line ?? HOOK_FAILURE_UNKNOWN;
}
