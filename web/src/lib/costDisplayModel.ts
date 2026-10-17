// Pure half of lib/costDisplay.tsx (kept in a .ts file so the node test runner
// can import it). See costDisplay.tsx for the ruling and the boot order.

export type CostDisplayModeName = "usd" | "tokens";

export interface CostDisplayInfo {
  mode: CostDisplayModeName;
  // "default": no org choice applies (dollars); "org": the org's verified
  // signed choice; "pending": no authoritative answer yet (token-safe).
  source: "default" | "org" | "pending";
  org_name?: string;
  version?: number;
}

// PENDING_COST_DISPLAY is what the dashboard renders until GET
// /api/cost-display gives an authoritative answer (Sol A4-03): token usage,
// never dollars, so a stalled, failed or unreadable answer can never expose
// costs an org chose to hide from developers (D-013).
export const PENDING_COST_DISPLAY: CostDisplayInfo = { mode: "tokens", source: "pending" };

// normalizeCostDisplay returns the authoritative answer in a GET
// /api/cost-display response, or null when the response is not one (the gate
// then stays token-safe and retries). Only an explicit "usd" from a known
// source shows dollars; an org answer whose mode this build cannot read takes
// the restrictive token-usage mode rather than dollars.
export function normalizeCostDisplay(raw: unknown): CostDisplayInfo | null {
  if (!raw || typeof raw !== "object") return null;
  const r = raw as Record<string, unknown>;
  let mode: CostDisplayModeName;
  let source: "default" | "org";
  if (r.source === "org") {
    source = "org";
    mode = r.mode === "usd" ? "usd" : "tokens";
  } else if (r.source === "default" && (r.mode === "usd" || r.mode === "tokens")) {
    source = "default";
    mode = r.mode;
  } else {
    return null;
  }
  return {
    mode,
    source,
    org_name: typeof r.org_name === "string" && r.org_name ? r.org_name : undefined,
    version: typeof r.version === "number" ? r.version : undefined,
  };
}

// retryDelayMs is the wait before re-asking after the n-th failed attempt
// (0-based): doubling from 1 s, capped at 30 s. The read is a loopback call
// against an in-process cache, so the retry costs nothing.
export function retryDelayMs(attempt: number): number {
  const n = Math.max(0, Math.min(attempt, 5));
  return Math.min(1000 * 2 ** n, 30000);
}

// costDisplayNote is the one honest sentence the dashboard shows while costs
// are replaced by token usage ("" otherwise).
export function costDisplayNote(info: CostDisplayInfo): string {
  if (info.mode !== "tokens") return "";
  if (info.source === "pending") {
    return "Checking whether your organization shows costs or token usage. Token usage is shown until this dashboard can confirm it.";
  }
  const who = info.org_name ? info.org_name : "Your organization";
  return `${who} shows token usage instead of costs on developer dashboards. Figures that have no token count are hidden.`;
}

// ---------------------------------------------------------------------------
// Token-usage replacements for dollar-only wording. Pure, table-tested in
// costDisplay.test.ts.

// SOURCE_MIX_LABEL names a cost row's token source in the mix line; an
// unknown source counts as "other".
const SOURCE_MIX_LABEL: Readonly<Record<string, string>> = {
  proxy: "proxy",
  jsonl: "transcript",
  transcript: "transcript",
  mixed: "mixed",
};

export interface SourceMixRow {
  source: string;
  turn_count: number;
}

// tokenSourceMix is the token source mix of a window, by turns: "70% proxy ·
// 30% transcript". It replaces the cost-reliability subtitle of the Cost
// headline in token-usage mode ("" when no row carries a turn).
export function tokenSourceMix(rows: readonly SourceMixRow[]): string {
  const by = new Map<string, number>();
  let total = 0;
  for (const r of rows) {
    const n = r.turn_count > 0 ? r.turn_count : 0;
    if (!n) continue;
    const k = SOURCE_MIX_LABEL[r.source] ?? "other";
    by.set(k, (by.get(k) ?? 0) + n);
    total += n;
  }
  if (!total) return "";
  return [...by.entries()]
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .map(([k, n]) => `${Math.round((n / total) * 100)}% ${k}`)
    .join(" · ");
}

export interface BudgetShareInput {
  pct: number;
  budget_usd: number;
  forecast_usd: number;
}

// budgetShareLine is a budget scope as a share of its cap, never dollars:
// "62% of cap · day 2/31 · on pace 930%". The token-usage form of the
// budget card's "$mtd / $cap (pct · day · on pace $forecast)" line.
export function budgetShareLine(
  sc: BudgetShareInput,
  daysElapsed: number,
  daysInMonth: number,
): string {
  const parts = [`${Math.round(sc.pct)}% of cap`, `day ${daysElapsed}/${daysInMonth}`];
  const pace = budgetPacePct(sc);
  if (pace != null) parts.push(`on pace ${pace}%`);
  return parts.join(" · ");
}

// budgetPacePct is the linear month-end forecast as a whole-number share of
// the cap, or null when there is no cap or no forecast. The one pace rule
// the budget card and the budget banner share.
export function budgetPacePct(sc: BudgetShareInput): number | null {
  if (!(sc.budget_usd > 0) || !Number.isFinite(sc.forecast_usd) || !(sc.forecast_usd > 0)) {
    return null;
  }
  return Math.round((sc.forecast_usd / sc.budget_usd) * 100);
}

export interface BudgetBannerInput extends BudgetShareInput {
  mtd_usd: number;
}

// budgetBannerSentence is the local budget banner's sentence. With `money`
// (the dollar formatter) it is the dollar copy; with null, the org's
// token-usage view, it reads in shares of the cap through the same pace rule
// as budgetShareLine and names no amount, worded like the org budget banner's
// token-mode sentence ("NN% used of the ... budget", budgetWarning.ts
// noticeMessage and the daemon's budgetwarn.RenderMessage). The pace clause
// appears only when the forecast passes the cap, in both forms.
export function budgetBannerSentence(
  label: string,
  sc: BudgetBannerInput,
  money: ((usd: number) => string) | null,
): string {
  const overPace = sc.forecast_usd > sc.budget_usd;
  if (money) {
    return `${label} is at ${money(sc.mtd_usd)} of its ${money(sc.budget_usd)} monthly budget${
      overPace ? ` - on pace for ${money(sc.forecast_usd)} by month end` : ""
    }. Advisory only - nothing is blocked.`;
  }
  const pace = budgetPacePct(sc);
  return `${label}: ${Math.round(sc.pct)}% used of the monthly budget${
    overPace && pace != null ? `, on pace for ${pace}% by month end` : ""
  }. Advisory only, nothing is blocked.`;
}
