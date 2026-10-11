// Per-tab visibility (backlog item 4, T-132): the SPA half.
//
// An org can hide one TAB of a page it otherwise shows ("show Security, hide
// its Prompt guard tab"). The daemon resolves which tabs are hidden on this
// machine and serves them as GET /api/governance's `visibility.hidden_tabs`
// ("<page>/<tab>" ids, the page's nav id and the tab's `?tab=` id from
// lib/nodePageTabs.ts). The daemon also refuses the routes a hidden tab owns,
// so this file is the view half only: drop the tab from its strip, from the
// palette's tab jumps, and never land on it through a deep link.
//
// Pure: no React, no fetch, so tabVisibility.test.ts runs under `node --test`.
// With nothing hidden every helper is the identity, which is the individual
// node guarantee.

import type { PageTabs } from "./nodePageTabs.ts";

/** TAB_ID_SEP separates the page id from the tab id (nodegov.NavTabSep). */
export const TAB_ID_SEP = "/";

/** tabId renders the governance id of one page tab. */
export function tabId(navId: string, tab: string): string {
  return `${navId}${TAB_ID_SEP}${tab}`;
}

type GovWithVisibility = { visibility?: { hidden_tabs?: string[] } } | null | undefined;

const NONE: readonly string[] = [];

/** hiddenTabIds is the "<page>/<tab>" ids hidden on this machine. */
export function hiddenTabIds(gov: GovWithVisibility): readonly string[] {
  return gov?.visibility?.hidden_tabs ?? NONE;
}

/**
 * withVisibleTabs returns `tabs` without the tabs hidden for page `navId`, in
 * strip order, so `ids[0]` (the default tab useUrlTab falls back to) is the
 * first tab still shown. It returns `tabs` itself when nothing on the page is
 * hidden. A page whose every tab is hidden is hidden by the daemon as a whole;
 * the strip then keeps its full table rather than render no tab at all.
 */
export function withVisibleTabs<T extends string>(
  navId: string,
  tabs: PageTabs<T>,
  hidden: readonly string[],
): PageTabs<T> {
  if (hidden.length === 0) return tabs;
  const set = new Set(hidden);
  const ids = tabs.ids.filter((id) => !set.has(tabId(navId, id)));
  if (ids.length === tabs.ids.length || ids.length === 0) return tabs;
  return { ids, labels: tabs.labels };
}

/**
 * tabOfPath maps a SPA address ("/security?tab=rules") onto its tab id, using
 * the tabbed pages' own paths; the page's default tab when the address names
 * none. null for a page without tabs.
 */
export function tabOfPath(
  path: string,
  routes: ReadonlyArray<{ navId: string; path: string; tabs: { ids: readonly string[] } }>,
): string | null {
  const [pathname, query = ""] = path.split("?", 2);
  const r = routes.find((x) => x.path === pathname);
  if (!r) return null;
  const tab = new URLSearchParams(query.split("#", 1)[0]).get("tab");
  const id = tab && r.tabs.ids.includes(tab) ? tab : r.tabs.ids[0];
  return id ? tabId(r.navId, id) : null;
}

/** withoutHiddenTabPaths drops the items whose address lands on a hidden tab. */
export function withoutHiddenTabPaths<I>(
  items: readonly I[],
  pathOf: (item: I) => string | undefined,
  hidden: readonly string[],
  routes: ReadonlyArray<{ navId: string; path: string; tabs: { ids: readonly string[] } }>,
): I[] {
  if (hidden.length === 0) return items.slice();
  const set = new Set(hidden);
  return items.filter((it) => {
    const p = pathOf(it);
    if (!p) return true;
    const id = tabOfPath(p, routes);
    return id == null || !set.has(id);
  });
}
