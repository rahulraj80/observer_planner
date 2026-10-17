import type { PreflightRuntime, ToolPreflight } from "./types.ts";

// Pure copy helpers for the minimum-runtime check (the preflight's
// launch_runtime / install_runtime). The server owns the decision and the
// wording (internal/toolinstall CheckRuntime) and sends it in parts: a short
// summary ("Gemini CLI needs Node.js 20 or newer. This machine has 18.19.1."),
// the interpreter path, the grounded command that installs a newer runtime on
// this host, and a one-line note. This module only picks them, and still says
// something useful if an older server sends the status without the parts.

/** The parts the dialog renders for one too_old check. */
export type RuntimeRemedyParts = {
  /** Short sentence: what the tool needs and what this machine has. */
  summary: string;
  /** The interpreter that was checked, shown in small text ("" = unknown). */
  path: string;
  /** "Node.js", for the "Install a newer Node.js first:" lead-in. */
  display: string;
  /** The command on its own, for a monospace block with a copy button ("" = none grounded). */
  command: string;
  /** One-line context for the command, or the whole advice when there is no command. */
  note: string;
};

// fallbackSummary is the short sentence for a too_old check an older server
// sent without one.
function fallbackSummary(r: PreflightRuntime): string {
  const who = r.product?.trim() || "This tool";
  const have = r.have ? ` This machine has ${r.have}.` : "";
  return `${who} needs ${r.display} ${r.min} or newer.${have}`;
}

/** runtimeRemedyParts splits one check into its parts, or null unless too_old. */
export function runtimeRemedyParts(r: PreflightRuntime | undefined | null): RuntimeRemedyParts | null {
  if (!r || r.status !== "too_old") return null;
  const command = r.fix_command?.trim() ?? "";
  let note = r.fix_note?.trim() ?? "";
  // An older server sent only the plain-text remedy: show it as the note so
  // nothing is lost.
  if (!command && !note && !r.summary && r.remedy?.trim()) note = r.remedy.trim();
  if (!command && !note) note = `Install a newer ${r.display} first, then press Re-check.`;
  return {
    summary: r.summary?.trim() || fallbackSummary(r),
    path: r.path?.trim() ?? "",
    display: r.display,
    command,
    note,
  };
}

/**
 * launchRuntimeParts: the installed tool's interpreter is older than its
 * grounded minimum, so the launcher would refuse it. null otherwise.
 */
export function launchRuntimeParts(preflight: ToolPreflight | null): RuntimeRemedyParts | null {
  return runtimeRemedyParts(preflight?.launch_runtime);
}

/**
 * installRuntimeParts: the guided install would put the tool on a host
 * runtime older than its grounded minimum, so the server withholds
 * can_install. null otherwise.
 */
export function installRuntimeParts(preflight: ToolPreflight | null): RuntimeRemedyParts | null {
  return runtimeRemedyParts(preflight?.install_runtime);
}

/**
 * runtimeAdvisory is the non-blocking sentence for an outside_range check
 * (T-396): the interpreter meets the vendor's floor, but the vendor's whole
 * declared range leaves its version out, so the tool may fail to start. It
 * never blocks a launch or an install (review M3-18: "may under-block, never
 * over-block"); the doctor says the same. null for every other status.
 */
export function runtimeAdvisory(r: PreflightRuntime | undefined | null): string | null {
  if (!r || r.status !== "outside_range") return null;
  const said = r.summary?.trim();
  if (said) return said;
  const who = r.product?.trim() || "This tool";
  const range = r.range?.trim() ? `${r.display} ${r.range.trim()}` : `a ${r.display} version range`;
  const have = r.have ? ` This machine has ${r.have}, which that range leaves out,` : " This machine's version is outside it,";
  return `${who} declares ${range}.${have} so the tool may fail to start.`;
}

/** launchRuntimeAdvisory: the installed tool's runtime is outside the declared range (never blocks). */
export function launchRuntimeAdvisory(preflight: ToolPreflight | null): string | null {
  return runtimeAdvisory(preflight?.launch_runtime);
}

/** installRuntimeAdvisory: the guided install's host runtime is outside the declared range (never blocks). */
export function installRuntimeAdvisory(preflight: ToolPreflight | null): string | null {
  return runtimeAdvisory(preflight?.install_runtime);
}

/** launchRuntimeRemedy is the short sentence for a blocked launch, or null. */
export function launchRuntimeRemedy(preflight: ToolPreflight | null): string | null {
  return launchRuntimeParts(preflight)?.summary ?? null;
}

/** installRuntimeRemedy is the short sentence for a refused install, or null. */
export function installRuntimeRemedy(preflight: ToolPreflight | null): string | null {
  return installRuntimeParts(preflight)?.summary ?? null;
}
