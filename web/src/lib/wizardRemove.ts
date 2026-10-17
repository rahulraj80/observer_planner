// wizardRemove: the setup wizard's Remove panel reading of one removal reply
// (POST /api/setup/hooks|mcp {"undo": true} and POST /api/setup/claude|codex
// {"undo": true}), the dashboard twin of `observer uninstall --<tool>`. Pure
// (no React, no imports), so the node test runner covers it.
//
// Rules this module exists for:
//   - A reply that is not a 200 is never rendered as a plan (a 403 from the
//     confirm-token / local-only guard is an error, a 409 is a conflict the
//     Force button can override).
//   - The preview names the exact change: which entries go, from which file,
//     and which of the user's own entries the same file keeps.

export type RemoveStepId = "hooks" | "mcp" | "route" | "telemetry";

/** One reply shape covers the three removal endpoints. */
export type RemoveResponse = {
  config_path?: string;
  /** hooks / MCP: the entries removed; route: whether the route went. */
  removed?: string[] | boolean;
  kept?: string[];
  skipped?: boolean;
  base_url?: string;
  dry_run?: boolean;
  error?: string;
  /** telemetry step: the removal's own summary lines. */
  summary?: string[];
  changed?: boolean;
};

export type RemoveOutcome =
  | { kind: "plan"; nothing: boolean; line: string }
  | { kind: "conflict"; error: string }
  | { kind: "error"; error: string; denied: boolean };

function parse(text: string): { r: RemoveResponse; plain: string } {
  const trimmed = text.trim();
  try {
    const v: unknown = JSON.parse(trimmed);
    if (v && typeof v === "object" && !Array.isArray(v)) {
      return { r: v as RemoveResponse, plain: "" };
    }
  } catch {
    // plain-text body (http.Error): fall through
  }
  return { r: {}, plain: trimmed };
}

function plural(n: number, noun: string): string {
  return `${n} ${noun}${n === 1 ? "" : "s"}`;
}

function from(path: string | undefined): string {
  return path ? ` from ${path}` : "";
}

/** classifyRemoveReply turns one removal reply into what the step renders. */
export function classifyRemoveReply(
  step: RemoveStepId,
  status: number,
  bodyText: string,
  wrote: boolean,
): RemoveOutcome {
  const { r, plain } = parse(bodyText);
  if (status === 409) {
    return { kind: "conflict", error: r.error || plain || "conflict - the file changed since Observer wrote it" };
  }
  if (status !== 200) {
    const detail = r.error || plain || `HTTP ${status}`;
    if (status === 403) {
      return {
        kind: "error",
        denied: true,
        error: `this view may not ${wrote ? "remove" : "preview the removal of"} this step (${detail}); reload the panel, or open the dashboard on its own local address`,
      };
    }
    return { kind: "error", denied: false, error: `could not ${wrote ? "remove" : "preview the removal of"} this step: ${detail}` };
  }
  return { kind: "plan", ...summarizeRemoval(step, r, wrote) };
}

/** summarizeRemoval is a 200 reply's one-line preview or result. */
export function summarizeRemoval(
  step: RemoveStepId,
  r: RemoveResponse,
  wrote: boolean,
): { nothing: boolean; line: string } {
  const path = r.config_path || undefined;
  if (step === "route") {
    if (r.removed !== true) {
      const kept = r.base_url ? ` (the file points at ${r.base_url}, not this install's proxy; kept)` : "";
      return { nothing: true, line: `no proxy route of this install to remove${kept}` };
    }
    const verb = wrote ? "removed" : "will remove";
    const url = r.base_url ? ` ${r.base_url}` : "";
    return { nothing: false, line: `${verb} the proxy route${url}${from(path)}` };
  }
  if (step === "telemetry") {
    const what = (r.summary ?? []).join("; ");
    if (!r.changed) {
      return { nothing: true, line: `${what || "nothing of Observer's to remove"}${path ? ` in ${path}` : ""}` };
    }
    return { nothing: false, line: `${wrote ? "done: " : "will "}${what}${from(path)}` };
  }
  const removed = Array.isArray(r.removed) ? r.removed : [];
  const kept = Array.isArray(r.kept) ? r.kept : [];
  if (removed.length === 0) {
    return { nothing: true, line: `nothing of Observer's to remove${path ? ` in ${path}` : ""}` };
  }
  const what = step === "hooks" ? `${plural(removed.length, "hook event")} (${removed.join(", ")})` : "the observer MCP server";
  const keeps = kept.length > 0 ? `; keeps your own entries for ${kept.join(", ")}` : "";
  return { nothing: false, line: `${wrote ? "removed" : "will remove"} ${what}${from(path)}${keeps}` };
}
