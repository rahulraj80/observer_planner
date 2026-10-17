import { useRef, type ReactNode } from "react";
import clsx from "clsx";
import { Eye, type LucideIcon } from "lucide-react";
import { Icon } from "./Icon";
import { Tooltip } from "./Tooltip";
import {
  AUDITED_SR_SUFFIX,
  auditedNote,
  focusableTabId,
  latchVisited,
  nextTabId,
  tabPanelState,
  shownTabs,
} from "./tabsModel";
import { StripEndPad, useStripScroll } from "./useStripScroll";

// TabStrip + TabPanel: the platform's one tab pattern
// (docs/app-design-system.md "Page tabs"; when to use page tabs vs a section
// rail vs jump chips is decided there, not per page).
//
// WHY A PRIMITIVE. SegmentedControl (the other `role=tablist`) is a compact
// 2-3 option pill toggle sized for a table header, not a navigation bar. Every
// surface that needs tabs extends this ONE implementation (CLAUDE.md "fix
// systemically" / module-boundary rule 1). The rules it applies (keyboard
// navigation, usable tabs, the audited copy, the keep-alive latch) live in
// the pure tabsModel.ts, table-tested in node.
//
// PURE. No fetch, no router, no app state. URL state belongs to the app's
// hook (web2 `useUrlEnum`, web `useUrlTab`); counts and dots are whatever the
// caller passes. NEVER fire an audited read just to compute a count.
//
// HONEST DISABLING. A disabled tab REQUIRES `disabledReason`, which becomes
// the tooltip and must name the exact missing dependency ("not configured on
// this server"). A tab the viewer has no PERMISSION for is not disabled: it is
// `hidden` (not rendered, skipped by the keyboard).
//
// AUDITED. `audited: { actions }` marks a tab whose data the server writes to
// the audit log when it loads: an eye glyph after the label, a tooltip that
// names the actions, and "(audited)" in the accessible name. The marker does
// not load anything; the panel's own loader does, on first activation.
//
// MOBILE. The strip scrolls horizontally rather than wrapping (one row at
// every width). The active tab is scrolled into view on mount, on every
// change (a cold deep link to the 6th tab does not open with it off-screen)
// and whenever the strip's content or size changes (a count arriving later),
// and an overflowing end fades out (a CSS mask, so it works on any
// background); a trailing pad lets the last tab scroll clear of that fade. `scrollbar-none` is deliberately NOT used so a desktop
// trackpad user still sees the affordance.

export type TabDef<T extends string> = {
  id: T;
  /** The visible label. A string, or a node (a label with a badge, a mark).
   *  A node label is never stringified: give `ariaLabel` so the tab keeps a
   *  plain-text accessible name. */
  label: ReactNode;
  /** Plain-text accessible name, for a tab whose `label` is a node. */
  ariaLabel?: string;
  /** Optional lucide glyph before the label (drawn through <Icon>). */
  icon?: LucideIcon;
  /** Optional trailing count (e.g. the message total). Rendered in mono.
   *  Only from data the page already loaded, or a cheap non-audited count. */
  count?: number | null;
  /** When true the tab is not selectable; `disabledReason` is then required. */
  disabled?: boolean;
  /** Names the exact missing dependency. Shown as the tab's title tooltip. */
  disabledReason?: string;
  /** Optional trailing dot, for "this tab has something notable". Same data
   *  rule as `count`. */
  dot?: "warn" | "danger" | null;
  /** The viewer may not see this tab (no permission): it is not rendered and
   *  the keyboard skips it. Pages resolve their value with `resolveTab`. */
  hidden?: boolean;
  /** Opening this tab loads data the server records in the audit log. The
   *  `actions` are the audit action names its loaders write
   *  (e.g. ["view_guard_posture"]); they appear in the tooltip. */
  audited?: { actions: string[] };
};

/** tabDomId is the `id` TabStrip gives a tab button (`aria-labelledby` target). */
export function tabDomId(idPrefix: string, id: string): string {
  return `${idPrefix}-tab-${id}`;
}

/** panelDomId is the `id` of a tab's panel (the tab's `aria-controls`). */
export function panelDomId(idPrefix: string, id: string): string {
  return `${idPrefix}-panel-${id}`;
}

export function TabStrip<T extends string>({
  tabs,
  value,
  onChange,
  idPrefix,
  ariaLabel = "Sections",
  className,
}: {
  tabs: TabDef<T>[];
  value: T;
  onChange: (id: T) => void;
  /** Prefix for the generated `id` / `aria-controls` pair. Must match the
   *  `idPrefix` given to this strip's TabPanels. */
  idPrefix: string;
  /** Accessible name of the tablist (e.g. "Session detail sections"). */
  ariaLabel?: string;
  /** Applied to the strip's outer box: spacing (`mb-4`), width. */
  className?: string;
}): ReactNode {
  const stripRef = useRef<HTMLDivElement>(null);
  const shown = shownTabs(tabs);
  const focusable = focusableTabId(shown, value);
  const { maskStyle, endPad } = useStripScroll(stripRef, focusable, (strip) =>
    focusable == null
      ? null
      : strip.querySelector<HTMLElement>(`#${CSS.escape(tabDomId(idPrefix, focusable))}`),
  );

  // Roving arrow-key navigation across the usable tabs, per the WAI-ARIA
  // tabs pattern (automatic activation). Home/End jump to the ends.
  function onKeyDown(e: React.KeyboardEvent<HTMLDivElement>) {
    const target = nextTabId(shown, value, e.key);
    if (target == null) return;
    e.preventDefault();
    onChange(target);
    stripRef.current
      ?.querySelector<HTMLButtonElement>(`#${CSS.escape(tabDomId(idPrefix, target))}`)
      ?.focus();
  }

  return (
    // The under-bar border sits on the outer box so the edge-fade mask on the
    // scroller does not fade the line itself. min-w-0 lets the strip shrink
    // (and scroll) inside a flex row.
    <div className={clsx("min-w-0 border-b border-line-2", className)}>
      <div
        ref={stripRef}
        role="tablist"
        aria-label={ariaLabel}
        onKeyDown={onKeyDown}
        style={maskStyle}
        className="relative flex items-stretch gap-0.5 overflow-x-auto"
      >
        {shown.map((t) => {
          const active = t.id === value && !t.disabled;
          const audited = t.audited != null && !t.disabled;
          const button = (
            <button
              key={t.id}
              id={tabDomId(idPrefix, t.id)}
              type="button"
              role="tab"
              aria-selected={active}
              aria-label={
                t.ariaLabel != null && audited ? `${t.ariaLabel} ${AUDITED_SR_SUFFIX}` : t.ariaLabel
              }
              aria-controls={panelDomId(idPrefix, t.id)}
              aria-disabled={t.disabled || undefined}
              // Only one tab is in the Tab order; arrow keys move within the
              // strip (WAI-ARIA roving tabindex).
              tabIndex={t.id === focusable ? 0 : -1}
              disabled={t.disabled}
              title={t.disabled ? t.disabledReason : undefined}
              onClick={() => !t.disabled && onChange(t.id)}
              className={clsx(
                "inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap border-b-2 px-3 py-2 text-[12px] transition-colors focus:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-accent-ring",
                t.disabled
                  ? "cursor-not-allowed border-transparent text-fg-4"
                  : active
                    ? "border-accent font-semibold text-fg-0"
                    : "border-transparent text-fg-3 hover:text-fg-1",
              )}
            >
              {t.icon && (
                <Icon
                  icon={t.icon}
                  size="xs"
                  className={clsx("shrink-0", active ? "text-accent" : "text-fg-4")}
                />
              )}
              {t.label}
              {audited && (
                <>
                  <Icon
                    icon={Eye}
                    size="xs"
                    aria-hidden
                    data-audited-marker=""
                    className={clsx("shrink-0", active ? "text-fg-2" : "text-fg-4")}
                  />
                  {t.ariaLabel == null && <span className="sr-only">{AUDITED_SR_SUFFIX}</span>}
                </>
              )}
              {t.count != null && (
                <span className="font-mono text-[10px] text-fg-4 tabular-nums">{t.count}</span>
              )}
              {t.dot && (
                <span
                  aria-hidden
                  className={clsx(
                    "inline-block h-1.5 w-1.5 rounded-full align-middle",
                    t.dot === "danger" ? "bg-danger" : "bg-warn",
                  )}
                />
              )}
            </button>
          );
          return audited ? (
            <Tooltip key={t.id} content={auditedNote(t.audited!.actions)} side="bottom">
              {button}
            </Tooltip>
          ) : (
            button
          );
        })}
        {endPad && <StripEndPad />}
      </div>
    </div>
  );
}

/**
 * TabPanel is one tab's panel with keep-alive semantics: it renders nothing
 * until its tab is first activated (so the panel's loaders, including audited
 * ones, do not run before the viewer opens it), then stays mounted for the
 * life of the strip, hidden when inactive. Switching back neither refetches
 * nor re-audits, and loaded data, expanded rows and scroll positions survive.
 * Effects still run while hidden, so a time-range change or a TopBar Refresh
 * refetches the panels already visited (a fresh disclosure, correctly
 * re-audited). Remount the panels (a React `key`) when the subject changes,
 * e.g. a drawer opened on a different session.
 *
 * Promoted from the session drawers' local TabPanel (web
 * SessionDetailPanel, web2 Sessions); `className` is applied while active
 * (the drawers pass "mt-5"; page tabs put the TabStrip's mb-4 above instead).
 */
export function TabPanel<T extends string>({
  id,
  active,
  idPrefix,
  className,
  children,
}: {
  id: T;
  /** The selected tab id (the TabStrip's `value`). */
  active: T;
  /** The TabStrip's `idPrefix`, so `aria-labelledby` resolves to the tab. */
  idPrefix: string;
  /** Classes for the panel while active. */
  className?: string;
  children: ReactNode;
}): ReactNode {
  const isActive = id === active;
  // Monotonic mount latch (a ref, not state: it needs no re-render of its own
  // and must survive the parent re-rendering with a different `active`).
  const visited = useRef(false);
  visited.current = latchVisited(visited.current, isActive);
  const state = tabPanelState(id, active, visited.current);
  if (state === "unmounted") return null;
  return (
    <div
      id={panelDomId(idPrefix, id)}
      role="tabpanel"
      aria-labelledby={tabDomId(idPrefix, id)}
      // Both the attribute and the utility class: the attribute keeps the
      // panel out of the accessibility tree, the class guarantees display:none
      // regardless of any base-layer rule that might set `display` on a div.
      hidden={state === "hidden"}
      className={state === "active" ? className : "hidden"}
    >
      {children}
    </div>
  );
}

/**
 * useTabVisited is the keep-alive latch for a loader that lives OUTSIDE a
 * TabPanel (a page-level hook feeding several panels): false until `id` is
 * first active, then true for the life of the component. Gate the loader on
 * it, e.g. `useApi(visited ? loader : null, ...)`, so an audited read still
 * fires on first open only.
 */
export function useTabVisited<T extends string>(id: T, active: T): boolean {
  const visited = useRef(false);
  visited.current = latchVisited(visited.current, id === active);
  return visited.current;
}
