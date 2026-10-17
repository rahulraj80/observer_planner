// sectionRailModel.ts - the PURE half of SectionRail (shared/primitives/
// SectionRail.tsx): which groups and items render, and which section a
// `?section=` value selects. No React, no DOM, table-tested in node
// (web/src/lib/sectionRail.test.ts).

/** The fields of a rail item the selection rules read. SectionRailItem satisfies it. */
export type RailItemRuleInput<T extends string = string> = {
  id: T;
  /** The viewer may not see this section (permission, governance): not rendered. */
  hidden?: boolean;
  /** A LINK row: it leaves the page instead of selecting a section. */
  href?: string;
  /** A link row's plain-click action (see SectionRailItem.onSelect). */
  onSelect?: () => void;
};

/**
 * isRailLink is true for a LINK row (one with `href` and/or `onSelect`): it
 * navigates somewhere else, so it is never the selected section, never
 * `aria-current`, and `pickRailSection` never lands on it.
 */
export function isRailLink(item: RailItemRuleInput): boolean {
  return item.href != null || item.onSelect != null;
}

/**
 * interceptLinkClick is true when a click on a link row should run the row's
 * in-app action (onSelect, else the rail's onChange) instead of following the
 * href: a plain, unmodified primary-button click. A middle click, or one with
 * Ctrl / Cmd / Shift / Alt (new tab, new window, download), or a click whose
 * default something else already prevented, is left to the browser, which is
 * what makes "open in new tab" work on a link row.
 */
export function interceptLinkClick(e: {
  button: number;
  metaKey: boolean;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
  defaultPrevented?: boolean;
}): boolean {
  return !e.defaultPrevented && e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey;
}

/** The fields of a rail group the selection rules read. */
export type RailGroupRuleInput<I extends RailItemRuleInput> = {
  id: string;
  items: readonly I[];
};

/**
 * shownRailGroups drops hidden items, then any group left with no items, so
 * an empty group never renders as an orphan heading. Order is kept.
 */
export function shownRailGroups<I extends RailItemRuleInput, G extends RailGroupRuleInput<I>>(
  groups: readonly G[],
): (Omit<G, "items"> & { items: I[] })[] {
  return groups
    .map((g) => ({ ...g, items: g.items.filter((i) => !i.hidden) }))
    .filter((g) => g.items.length > 0);
}

/** railItems is every shown item in rail order (the chip row's order). */
export function railItems<I extends RailItemRuleInput>(
  groups: readonly RailGroupRuleInput<I>[],
): I[] {
  return shownRailGroups<I, RailGroupRuleInput<I>>(groups).flatMap((g) => g.items);
}

/**
 * pickRailSection is the section a `?section=` value selects: `raw` when it
 * names a shown section, else `fallback` when that is shown, else the first
 * shown section, else null (nothing to show). A stale or forbidden deep link
 * lands on a real section instead of an empty pane. Link rows (`isRailLink`)
 * are not sections and are never picked.
 */
export function pickRailSection<T extends string>(
  raw: string | null | undefined,
  groups: readonly RailGroupRuleInput<RailItemRuleInput<T>>[],
  fallback?: T,
): T | null {
  const ids = railItems(groups)
    .filter((i) => !isRailLink(i))
    .map((i) => i.id);
  if (raw != null && (ids as string[]).includes(raw)) return raw as T;
  if (fallback != null && ids.includes(fallback)) return fallback;
  return ids[0] ?? null;
}
