import type { ToolPreflight } from "./types.ts";

// Pure copy helper for the New terminal dialog's "installer prerequisites
// missing" box. The server owns the decision and the sentence (it checks the
// registry's grounded InstallHint.Requires against this host and names the
// package-manager command for the detected OS family, see
// internal/toolinstall/prereq.go); this module only picks what to show, and
// still says something useful if an older server sends the list without the
// sentence. A too-old host runtime (install_runtime) is rendered by its own
// box from runtimeCheck.ts, so its command can sit in a copyable block.

// installPrereqRemedyFor returns the remedy to render, or null when the
// install's prerequisites are all present.
export function installPrereqRemedyFor(preflight: ToolPreflight | null): string | null {
  if (!preflight) return null;
  const remedy = preflight.install_prereq_remedy?.trim();
  if (remedy) return remedy;
  const missing = (preflight.install_missing_prereqs ?? []).map((m) => m.split("|").join(" or ")).filter(Boolean);
  if (missing.length === 0) return null;
  const names = missing.length === 1 ? missing[0] : `${missing.slice(0, -1).join(", ")} and ${missing[missing.length - 1]}`;
  const [verb, pron] = missing.length === 1 ? ["is", "it"] : ["are", "them"];
  return `The installer needs ${names}, which ${verb} not installed on this machine. Install ${pron} with your system package manager, then press Re-check.`;
}
