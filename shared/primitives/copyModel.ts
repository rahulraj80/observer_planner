// copyModel: the pure half of the design system's copy and emphasis rules
// (docs/app-design-system.md "Copy rules" and "Emphasis"). The limits are
// data so a primitive, a test or a lint can read the same numbers the doc
// states, and the coverage / callout tones live here so a page never picks
// a colour for them by hand.

import type { Tone } from "../lib/tone";

/** Character limits for UI copy (about; a limit is a review flag, not a hard cut). */
export const COPY_LIMITS = {
  /** PageHeader `sub`: one line. */
  pageSubtitle: 110,
  /** A card's description under its title: one sentence. */
  cardDescription: 140,
  /** A SettingRow description or option consequence: one sentence. */
  settingDescription: 100,
  /** A label (field, setting, column, button). */
  label: 40,
  /** A Callout body. */
  callout: 160,
} as const;

export type CopyKind = keyof typeof COPY_LIMITS;

/** overCopyLimit reports how many characters `text` runs over its kind's limit (0 when within). */
export function overCopyLimit(text: string, kind: CopyKind): number {
  const n = text.trim().length;
  return Math.max(0, n - COPY_LIMITS[kind]);
}

/** Enforcement coverage of one budget, rule or control, as CoverageBadge shows it. */
export type CoverageStatus = "full" | "partial" | "none" | "unknown";

/** COVERAGE_TONE: only a row that is actually not covered gets a warn or danger colour. */
export const COVERAGE_TONE: Record<CoverageStatus, Tone> = {
  full: "success",
  partial: "warn",
  none: "danger",
  unknown: "neutral",
};

/** coverageFromCounts turns "covered of total" into a status (total 0 is unknown, never full). */
export function coverageFromCounts(covered: number, total: number): CoverageStatus {
  if (!Number.isFinite(covered) || !Number.isFinite(total) || total <= 0) return "unknown";
  if (covered >= total) return "full";
  if (covered <= 0) return "none";
  return "partial";
}

/** coverageCountLabel renders "1 of 3"; an all or nothing count stays a count too. */
export function coverageCountLabel(covered: number, total: number): string {
  return `${Math.max(0, Math.round(covered))} of ${Math.max(0, Math.round(total))}`;
}

/** Callout tones. A warn or danger callout is for a condition to act on, never an explanation. */
export type CalloutTone = "info" | "warn" | "danger" | "success";

/** isLoudCallout: the tones the "one per view" emphasis rule counts. */
export function isLoudCallout(tone: CalloutTone): boolean {
  return tone === "warn" || tone === "danger";
}

/**
 * loudCalloutsOverBudget counts how many warn or danger callouts in one view
 * exceed the emphasis rule's budget of one. Pages and tests can call it with
 * the tones they are about to render.
 */
export function loudCalloutsOverBudget(tones: readonly CalloutTone[]): number {
  return Math.max(0, tones.filter(isLoudCallout).length - 1);
}

/** The storage seam InfoDisclosure reads its remembered open state through. */
export type DisclosureStorage = {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
};

const DISCLOSURE_PREFIX = "sb.disclosure.";

/** readDisclosureOpen returns the remembered open state, or `fallback` when none or unreadable. */
export function readDisclosureOpen(store: DisclosureStorage | null | undefined, key: string, fallback: boolean): boolean {
  if (!store || !key) return fallback;
  try {
    const v = store.getItem(DISCLOSURE_PREFIX + key);
    if (v === "1") return true;
    if (v === "0") return false;
    return fallback;
  } catch {
    return fallback;
  }
}

/** writeDisclosureOpen remembers the open state; a blocked store is ignored. */
export function writeDisclosureOpen(store: DisclosureStorage | null | undefined, key: string, open: boolean): void {
  if (!store || !key) return;
  try {
    store.setItem(DISCLOSURE_PREFIX + key, open ? "1" : "0");
  } catch {
    // Private windows and blocked site data: the disclosure still works, it just forgets.
  }
}
