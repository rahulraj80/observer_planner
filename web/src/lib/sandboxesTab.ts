// sandboxesTab.ts - the pure half of the Terminals > Sandboxes tab
// (components/SandboxesTab.tsx): its place in the Terminals tab strip, and
// the words it shows. No React, no imports, so sandboxesTab.test.ts runs
// under plain `node --test`.
//
// The tab is the observer_planner fork's own. Like the fork's own nav pages
// (internal/policyfam/nodegov/vocab_source_test.go forkOnlySectionIDs) it is
// deliberately NOT in nodePageTabs.ts: a tab there joins the org-governance
// vocabulary (navtabs_gen.go) and would bump CurrentVocabLevel, a contract
// with the enterprise org server. Its data routes sit under the governed
// Terminals section, so hiding Terminals still withholds them.
//
// One vocabulary on every screen (SandHold's): isolation is Shared,
// Shielded, Sealed or Vault; network is None, Limited or Open.

/** The fork's tabs on the Terminals page, inserted after Workspace. */
export const TERMINALS_FORK_TABS = {
  ids: ["sandboxes"] as const,
  labels: { sandboxes: "Sandboxes" } as Record<"sandboxes", string>,
};
export type TerminalsForkTab = (typeof TERMINALS_FORK_TABS.ids)[number];

/** withForkTabs puts the fork's tabs after the first (default) tab. */
export function withForkTabs<T extends string>(ids: readonly T[]): (T | TerminalsForkTab)[] {
  if (ids.length === 0) return [...TERMINALS_FORK_TABS.ids];
  return [ids[0], ...TERMINALS_FORK_TABS.ids, ...ids.slice(1)];
}

/** forkTabCrumb is the breadcrumb's tab segment for a fork tab, else null. */
export function forkTabCrumb(pathname: string, search: string): string | null {
  if (pathname !== "/terminals") return null;
  const v = new URLSearchParams(search).get("tab");
  return v === "sandboxes" ? TERMINALS_FORK_TABS.labels.sandboxes : null;
}

// Observer's isolation class -> SandHold's tier (internal/sandboxremote/
// sandhold/vocab.go classTier).
const CLASS_TIER: Record<string, string> = {
  process: "Shared",
  container: "Shielded",
  userspace_kernel: "Sealed",
  hw_vm: "Vault",
};

/** tierForClass is an isolation class in SandHold's words ("" for none). */
export function tierForClass(cls: string | undefined): string {
  return (cls && CLASS_TIER[cls]) || "";
}

/** tierTitle capitalises a node tier word ("shielded" -> "Shielded"). */
export function tierTitle(tier: string | undefined): string {
  if (!tier) return "";
  return tier.charAt(0).toUpperCase() + tier.slice(1).toLowerCase();
}

// Observer's network tier (sandboxvocab) -> SandHold's network word.
const EGRESS_NETWORK: Record<string, string> = {
  none: "None",
  proxy_only: "Limited",
  internet: "Open",
  host: "Open (host network)",
};

/** networkForEgress is a run's network tier in SandHold's words. */
export function networkForEgress(tier: string | undefined): string {
  if (!tier) return "";
  return EGRESS_NETWORK[tier] ?? tier;
}

/** networkWord capitalises a node network word ("limited" -> "Limited"). */
export function networkWord(n: string): string {
  return tierTitle(n);
}

/** durationText is a run's length: "48 s", "14 min", "2 h 5 min". */
export function durationText(seconds: number | undefined): string {
  if (seconds === undefined || seconds < 0) return "";
  if (seconds < 60) return `${Math.round(seconds)} s`;
  const m = Math.round(seconds / 60);
  if (m < 60) return `${m} min`;
  return `${Math.floor(m / 60)} h ${m % 60} min`;
}

/** One run of GET /api/terminal/sandbox/runs. */
export type SandboxRun = {
  run_id: string;
  tool: string;
  kind: string;
  launched_at: string;
  ended_at?: string;
  duration_seconds?: number;
  state: string;
  exit_code?: number;
  end_reason?: string;
  backend: string;
  stack?: string[];
  isolation_class: string;
  egress_tier?: string;
  capture_level?: string;
  refused?: number;
  refused_may_be_more?: boolean;
};

/**
 * findingsText is what Observer recorded for a run: the connections its
 * sandbox refused. Empty when nothing is on record, never an invented
 * "nothing found".
 */
export function findingsText(r: SandboxRun): string {
  const n = r.refused ?? 0;
  if (n > 0) {
    const s = `${n} connection${n === 1 ? "" : "s"} refused`;
    return r.refused_may_be_more ? `${s}, maybe more` : s;
  }
  return r.refused_may_be_more ? "connections may have been refused" : "";
}

/** statusText is a run's state in a few words. */
export function statusText(r: SandboxRun): string {
  if (r.state === "running") return "running";
  if (r.state === "abandoned") return "lost (no live terminal)";
  if (r.exit_code !== undefined && r.exit_code !== null) return `ended, exit ${r.exit_code}`;
  return "ended";
}

/** One question of the node's advisor (GET /v1/advise, in its words). */
export type AdviseChoice = { id: string; label: string; means?: string; hosts?: string[] };
export type AdviseQuestion = { id: string; ask: string; choices: AdviseChoice[] };

/** The answers the panel holds: one choice for runs and trust, a set for reach. */
export type AdviseAnswers = { runs: string; trust: string; reach: string[] };

/** adviseReady says whether the two single-choice questions are answered. */
export function adviseReady(a: AdviseAnswers): boolean {
  return a.runs !== "" && a.trust !== "";
}

/**
 * adviseBody is the POST body: the reach choices are package sources by id
 * (the node opens their hosts), extra hosts typed by the person are hosts.
 */
export function adviseBody(a: AdviseAnswers, extraHosts: string): {
  runs: string;
  trust: string;
  reach: { packages: string[]; hosts: string[] };
} {
  const hosts = extraHosts
    .split(/[\s,]+/)
    .map((h) => h.trim().toLowerCase())
    .filter((h) => h !== "");
  return { runs: a.runs, trust: a.trust, reach: { packages: [...a.reach], hosts: [...new Set(hosts)] } };
}

/** nodeError reads the daemon's {error, fix} answer, else the raw text. */
export function nodeError(body: string): { error: string; fix?: string } {
  try {
    const v = JSON.parse(body) as { error?: unknown; fix?: unknown };
    if (typeof v.error === "string" && v.error !== "") {
      return { error: v.error, fix: typeof v.fix === "string" && v.fix !== "" ? v.fix : undefined };
    }
  } catch {
    // not JSON
  }
  return { error: body.trim() || "the request failed" };
}
