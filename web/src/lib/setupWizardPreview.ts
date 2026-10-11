// setupWizardPreview: the setup wizard's reading of one step's reply (the
// dry-run preview on mount and the real write on click) into what the step
// renders. Pure (no React, no imports), so the node test runner covers it
// (setupWizardPreview.test.ts).
//
// Two rules this module exists for:
//   - A reply that is not a 200 is never rendered as a plan. A 403 from the
//     daemon's CSRF / local-only guard comes back as plain text, which used to
//     parse as `{}` and read "will register 0 events in undefined".
//   - A step line never prints "undefined": a missing config path drops the
//     clause instead.

/** One response shape covers all three setup endpoints' fields. */
export type WizardResponse = {
  config_path?: string;
  hooks_added?: string[];
  already_set?: string[] | boolean;
  added?: boolean;
  dry_run?: boolean;
  error?: string;
  base_url?: string;
  /** telemetry step: what the edit does, and whether it changes the file. */
  summary?: string[];
  changed?: boolean;
  wired?: boolean;
};

export type WizardStepId = "hooks" | "mcp" | "route" | "telemetry";

/** What a step renders for one reply. */
export type WizardOutcome =
  | { kind: "plan"; done: boolean; line: string }
  | { kind: "conflict"; error: string }
  | { kind: "error"; error: string; denied: boolean };

/** parseWizardBody reads a reply body that may be JSON or plain text. */
export function parseWizardBody(text: string): { r: WizardResponse; plain: string } {
  const trimmed = text.trim();
  try {
    const v: unknown = JSON.parse(trimmed);
    if (v && typeof v === "object" && !Array.isArray(v)) {
      return { r: v as WizardResponse, plain: "" };
    }
  } catch {
    // plain-text body (http.Error): fall through
  }
  return { r: {}, plain: trimmed };
}

/** classifyWizardReply turns one HTTP reply into the step's outcome. */
export function classifyWizardReply(
  step: WizardStepId,
  status: number,
  bodyText: string,
  wrote: boolean,
): WizardOutcome {
  const { r, plain } = parseWizardBody(bodyText);
  if (status === 409) {
    return {
      kind: "conflict",
      error: r.error || plain || "conflict - this entry points somewhere you configured deliberately",
    };
  }
  if (status !== 200) {
    const detail = r.error || plain || `HTTP ${status}`;
    if (status === 403) {
      return {
        kind: "error",
        denied: true,
        error: `this view may not ${wrote ? "write" : "preview"} the setup (${detail}); open the dashboard on its own local address, or run observer init`,
      };
    }
    return { kind: "error", denied: false, error: `could not ${wrote ? "write" : "preview"} this step: ${detail}` };
  }
  return { kind: "plan", ...summarizeWizardStep(step, r, wrote) };
}

// where renders " <prep> <path>" for a known path and nothing otherwise.
function where(prep: string, path: string | undefined): string {
  return path ? ` ${prep} ${path}` : "";
}

function count(n: number, noun: string): string {
  return `${n} ${noun}${n === 1 ? "" : "s"}`;
}

/** summarizeWizardStep is a 200 reply's one-line plan or result. */
export function summarizeWizardStep(
  step: WizardStepId,
  r: WizardResponse,
  wrote: boolean,
): { done: boolean; line: string } {
  const path = r.config_path || undefined;
  switch (step) {
    case "hooks": {
      const added = r.hooks_added ?? [];
      const already = Array.isArray(r.already_set) ? r.already_set : [];
      if (added.length === 0 && already.length > 0) {
        return { done: true, line: `${count(already.length, "event")} registered${where("in", path)}` };
      }
      if (added.length === 0) {
        return { done: false, line: `no hook events to register${where("in", path)}` };
      }
      if (wrote) {
        return { done: true, line: `registered ${count(added.length, "event")}${where("in", path)}` };
      }
      const suffix = already.length > 0 ? ` (${already.length} already set)` : "";
      return { done: false, line: `will register ${count(added.length, "event")}${where("in", path)}${suffix}` };
    }
    case "mcp": {
      if (r.already_set === true) {
        return { done: true, line: `observer MCP server registered${where("in", path)}` };
      }
      if (wrote && r.added) {
        return { done: true, line: `registered${where("in", path)}` };
      }
      return { done: false, line: `will add the observer MCP server${where("to", path)}` };
    }
    case "route": {
      if (r.already_set === true) {
        const at = r.base_url || path;
        return { done: true, line: at ? `already routed (${at})` : "already routed" };
      }
      if (wrote && r.added) {
        const via = r.base_url ? ` via ${r.base_url}` : "";
        return { done: true, line: `routed${via}${path ? ` (${path})` : ""}` };
      }
      return { done: false, line: `will set ${r.base_url || "the proxy URL"}${where("in", path)}` };
    }
    case "telemetry": {
      const what = (r.summary ?? []).join("; ");
      if (!r.changed) {
        return { done: r.wired === true, line: `${what || "nothing to change"}${where("in", path)}` };
      }
      return { done: wrote, line: `${wrote ? "done: " : "will "}${what}${where("in", path)}` };
    }
  }
}
