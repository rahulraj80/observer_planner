// Page-access requests (T-435): a developer asks to see one dashboard page or
// page tab their organization hides on this machine. An approved grant shows
// it READ-ONLY until it ends (govern.Effective.GrantedPages on the daemon).
//
// Pure: no React, no router. Importable under `node --test`.

import type { DashboardVisibility } from "./visibility.ts";

/** One page or tab a developer may ask to see. */
export type PageChoice = { id: string; label: string };

type PageRow = { id: string; label: string };
type TabRoute = { navId: string; tabs: { ids: readonly string[]; labels: Readonly<Record<string, string>> } };

/**
 * hiddenPageChoices lists, in sidebar order, every page hidden on this
 * machine and every hidden tab (of a hidden page or of a shown one): exactly
 * what a page-access grant can show. Read-only pages are not listed (a grant
 * never unlocks a page), and neither is anything the posture leaves visible.
 */
export function hiddenPageChoices(
  vis: DashboardVisibility | null | undefined,
  pages: readonly PageRow[],
  tabRoutes: readonly TabRoute[],
): PageChoice[] {
  if (!vis) return [];
  const out: PageChoice[] = [];
  for (const p of pages) {
    if (vis.sections?.[p.id]?.level === "hidden") out.push({ id: p.id, label: `${p.label} page` });
    const route = tabRoutes.find((r) => r.navId === p.id);
    if (!route) continue;
    for (const tab of route.tabs.ids) {
      const id = `${p.id}/${tab}`;
      if (vis.tabs?.[id]?.level === "hidden") out.push({ id, label: `${p.label}: ${route.tabs.labels[tab] ?? tab} tab` });
    }
  }
  return out;
}

/** defaultPageChoice is the asked page when it is still hidden, else the first. */
export function defaultPageChoice(choices: readonly PageChoice[], asked?: string): string {
  if (asked && choices.some((c) => c.id === asked)) return asked;
  return choices[0]?.id ?? "";
}
