// tabsModel.ts - the PURE half of the TabStrip / TabPanel primitives
// (shared/primitives/Tabs.tsx). No React, no DOM: keyboard navigation, the
// usable-tab rules, the audited-marker copy and the keep-alive mount latch are
// decided here so they are table-tested in node (web/src/lib/tabsModel.test.ts)
// and cannot drift between the strip and the pages that resolve a tab value.

/** The fields of a tab that the navigation rules read. TabDef satisfies it. */
export type TabRuleInput<T extends string = string> = {
  id: T;
  disabled?: boolean;
  hidden?: boolean;
};

/** shownTabs drops the tabs the viewer may not see (`hidden`), keeping order. */
export function shownTabs<D extends TabRuleInput>(tabs: readonly D[]): D[] {
  return tabs.filter((t) => !t.hidden);
}

/** usableTabs is the shown tabs a user can select (not hidden, not disabled). */
export function usableTabs<D extends TabRuleInput>(tabs: readonly D[]): D[] {
  return tabs.filter((t) => !t.hidden && !t.disabled);
}

/**
 * resolveTab returns `value` when it names a usable tab, else the first usable
 * tab, else null (no tab can be shown). A page whose `?tab=` names a tab the
 * viewer cannot see (no permission) or cannot use (not configured) renders
 * the first usable one instead, never an empty panel.
 */
export function resolveTab<T extends string>(
  value: T | null | undefined,
  tabs: readonly TabRuleInput<T>[],
): T | null {
  const usable = usableTabs(tabs);
  if (value != null && usable.some((t) => t.id === value)) return value;
  return usable[0]?.id ?? null;
}

/** The keys a horizontal tablist handles (WAI-ARIA tabs pattern). */
export const TAB_NAV_KEYS = ["ArrowRight", "ArrowLeft", "Home", "End"] as const;

/**
 * nextTabId is the tab a key press moves to, or null when the key is not a
 * tablist key or there is nowhere to go. ArrowRight / ArrowLeft wrap around,
 * Home / End jump to the ends, and hidden or disabled tabs are skipped
 * (automatic activation: the strip both focuses and selects the result).
 * From a current value that is not usable, ArrowRight / Home land on the
 * first usable tab and ArrowLeft / End on the last.
 */
export function nextTabId<T extends string>(
  tabs: readonly TabRuleInput<T>[],
  current: T | null | undefined,
  key: string,
): T | null {
  const usable = usableTabs(tabs);
  const n = usable.length;
  if (n === 0) return null;
  const at = usable.findIndex((t) => t.id === current);
  let next: number;
  switch (key) {
    case "ArrowRight":
      next = at < 0 ? 0 : (at + 1) % n;
      break;
    case "ArrowLeft":
      next = at < 0 ? n - 1 : (at - 1 + n) % n;
      break;
    case "Home":
      next = 0;
      break;
    case "End":
      next = n - 1;
      break;
    default:
      return null;
  }
  return usable[next].id;
}

/**
 * focusableTabId is the one tab that sits in the Tab order (roving
 * tabindex): the selected tab when it is usable, else the first usable tab,
 * so the strip is always reachable from the keyboard.
 */
export function focusableTabId<T extends string>(
  tabs: readonly TabRuleInput<T>[],
  value: T | null | undefined,
): T | null {
  return resolveTab(value, tabs);
}

/** The screen-reader-only suffix appended to an audited tab's accessible name. */
export const AUDITED_SR_SUFFIX = "(audited)";

/**
 * auditedNote is the tooltip on an audited tab's eye marker: opening the tab
 * fetches data the server records in the audit log, and it names the actions
 * so an admin can find the entries. Duplicate and empty action names are
 * dropped; with none left it still says the open is recorded.
 */
export function auditedNote(actions: readonly string[]): string {
  const names = Array.from(new Set(actions.map((a) => a.trim()).filter((a) => a !== "")));
  const base = "Opening this tab is recorded in the audit log";
  return names.length === 0 ? `${base}.` : `${base} (${names.join(", ")}).`;
}

/**
 * The life of one keep-alive panel: "unmounted" until its tab is first
 * activated, then "active" while selected and "hidden" (mounted, display
 * none) after. It never returns to "unmounted": switching back neither
 * refetches nor re-audits.
 */
export type TabPanelState = "unmounted" | "active" | "hidden";

/**
 * tabPanelState is the TabPanel's render decision for one panel, given whether
 * it has been activated before (`visited`) and the selected tab.
 */
export function tabPanelState<T extends string>(id: T, active: T, visited: boolean): TabPanelState {
  if (id === active) return "active";
  return visited ? "hidden" : "unmounted";
}

/**
 * latchVisited is the monotonic mount latch: true once the panel's tab has
 * been the active one, and true forever after.
 */
export function latchVisited(visited: boolean, isActive: boolean): boolean {
  return visited || isActive;
}
