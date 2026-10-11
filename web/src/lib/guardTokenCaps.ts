// guardTokenCaps - the pure half of the Security page's token budget card
// (T-204). Under the token-usage developer cost display the node's guard
// budget reads and edits in TOKENS: [guard.budget] session_tokens /
// daily_tokens / weekly_tokens / monthly_tokens (rules B-621..B-624), one for
// one with the dollar caps. No React, no fetch: pinned by
// guardTokenCaps.test.ts.

/** TokenCapWindow is one token cap: its GET /api/guard/budget fields and its config key. */
export interface TokenCapWindow {
  id: "session" | "daily" | "weekly" | "monthly";
  label: string;
  /** The configured cap in the /api/guard/budget response. */
  capField: "session_tokens" | "daily_tokens" | "weekly_tokens" | "monthly_tokens";
  /** The window's usage in the response; none for the per-session cap (it is per session). */
  usageField?: "usage_today_tokens" | "usage_week_tokens" | "usage_month_tokens";
  /** The Go config field the save writes (GuardBudgetConfig). */
  configKey: "SessionTokens" | "DailyTokens" | "WeeklyTokens" | "MonthlyTokens";
}

/** TOKEN_CAP_WINDOWS is the card's row table, in display order. */
export const TOKEN_CAP_WINDOWS: readonly TokenCapWindow[] = [
  { id: "session", label: "Per session", capField: "session_tokens", configKey: "SessionTokens" },
  { id: "daily", label: "Per day", capField: "daily_tokens", usageField: "usage_today_tokens", configKey: "DailyTokens" },
  { id: "weekly", label: "Per 7 days", capField: "weekly_tokens", usageField: "usage_week_tokens", configKey: "WeeklyTokens" },
  { id: "monthly", label: "Per month", capField: "monthly_tokens", usageField: "usage_month_tokens", configKey: "MonthlyTokens" },
];

/** The largest cap the editor accepts (the config field is an int64). */
export const TOKEN_CAP_MAX = 1_000_000_000_000;

export type TokenCapDraft = { ok: true; value: number } | { ok: false; error: string };

/**
 * tokenCapDraft validates what the developer typed: a whole number of tokens,
 * 0 to turn the cap off, commas and underscores allowed as separators, and an
 * optional K / M / B suffix ("2M" is 2,000,000).
 */
export function tokenCapDraft(raw: string): TokenCapDraft {
  const s = raw.trim().replace(/[,_\s]/g, "");
  const m = /^(\d+(?:\.\d+)?)([kKmMbB]?)$/.exec(s);
  if (!m) return { ok: false, error: "Enter a whole number of tokens, for example 2000000 or 2M (0 turns the cap off)." };
  const mult: Record<string, number> = { "": 1, k: 1e3, m: 1e6, b: 1e9 };
  const value = Math.round(Number(m[1]) * mult[m[2].toLowerCase()]);
  if (!Number.isFinite(value) || value < 0) return { ok: false, error: "The cap cannot be negative." };
  if (value > TOKEN_CAP_MAX) return { ok: false, error: "That cap is larger than any window can use." };
  if (m[2] === "" && m[1].includes(".")) return { ok: false, error: "Tokens are whole numbers." };
  return { ok: true, value };
}

/** capUsePct is a window's usage as a share of its cap (null when the cap is off). */
export function capUsePct(used: number | undefined, cap: number | undefined): number | null {
  if (!cap || cap <= 0 || used == null || !Number.isFinite(used)) return null;
  return (used / cap) * 100;
}
