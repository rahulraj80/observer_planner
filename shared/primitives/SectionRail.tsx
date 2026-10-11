import { useRef, type MouseEvent, type ReactNode } from "react";
import clsx from "clsx";
import { ArrowUpRight, type LucideIcon } from "lucide-react";
import { Icon } from "./Icon";
import { interceptLinkClick, isRailLink, railItems, shownRailGroups } from "./sectionRailModel";
import { StripEndPad, useStripScroll } from "./useStripScroll";

// SectionRail: grouped vertical section navigation for a long CONFIGURATION
// page (7+ sections in groups, where a user comes to change one thing), one
// section shown at a time. Promoted from the node Settings page's local
// SectionNav (web/src/pages/Settings.tsx), which now renders through it.
//
// Distinct from SectionNav (sticky jump chips that scroll a long read-through
// page) and from TabStrip (3+ facets of one object). When to use which:
// docs/app-design-system.md "Page tabs".
//
// CONTROLLED AND PURE. No router: the page owns `value` (typically
// `?section=<id>` through web `useUrlTab(ids, fallback, { param: "section" })`
// or web2 `useUrlEnum("section", ids, fallback, { history: "push" })`) and
// resolves stale or hidden ids with `pickRailSection`. Rows carry whatever the
// caller passes in `trailing` (a status pill, a lock).
//
// LINK ROWS. An item with `href` and/or `onSelect` is a link row: it leaves
// the page instead of selecting a section (an "Elsewhere" row pointing at the
// page that owns a setting). With `href` it renders a real <a>, so middle
// click, Ctrl/Cmd-click and "open in new tab" work natively; a plain click
// runs `onSelect` (or, without one, reports the id through `onChange`, so a
// page that already routes link ids there keeps working) instead of a full
// page load, which keeps a router-driven app client-side. A link row shows the
// external arrow exactly once, is never `aria-current`, and `pickRailSection`
// never selects it.
//
// RESPONSIVE. At and above `collapseBelow` it is the vertical rail; below it,
// a single horizontally scrolling chip row (group headings become thin
// dividers, `trailing` is dropped) whose active chip is scrolled into view,
// with an edge fade when it overflows. Both are in the DOM; the inactive one
// is display:none, so only one navigation landmark is exposed at a time.

export type SectionRailItem<T extends string> = {
  id: T;
  label: string;
  /** The section's own glyph (the same one its card title shows). */
  icon?: LucideIcon;
  /** Right-hand slot in the vertical rail only (a status Pill, a lock). */
  trailing?: ReactNode;
  /** The viewer may not see this section (permission, governance). */
  hidden?: boolean;
  /** "danger" tints a destructive section (e.g. delete my data). */
  tone?: "danger";
  /** Makes this a LINK row rendered as a real <a href>: middle click and
   *  "open in new tab" follow it natively. A plain click runs `onSelect`, or
   *  without one reports the id through the rail's `onChange` (the page
   *  routes it client-side). */
  href?: string;
  /** Makes this a LINK row: its plain-click action (e.g. a router
   *  `navigate`). Without `href` the row is a button that runs it. */
  onSelect?: () => void;
};

export type SectionRailGroup<T extends string> = {
  id: string;
  /** Group heading. Omit for an unlabelled group. */
  label?: string;
  items: SectionRailItem<T>[];
};

// Tailwind needs the literal class names (no string building), so each
// breakpoint maps to its [rail, chip row] visibility classes.
const COLLAPSE: Record<"md" | "lg", readonly [string, string]> = {
  md: ["hidden md:flex", "md:hidden"],
  lg: ["hidden lg:flex", "lg:hidden"],
};

export function SectionRail<T extends string>({
  groups,
  value,
  onChange,
  ariaLabel = "Sections",
  collapseBelow = "md",
  className,
}: {
  groups: SectionRailGroup<T>[];
  /** The shown section (resolve it with pickRailSection first). */
  value: T;
  onChange: (id: T) => void;
  /** Accessible name of the navigation (e.g. "Settings sections"). */
  ariaLabel?: string;
  /** Below this breakpoint the rail becomes a chip row. Match the page grid's
   *  breakpoint (the node Settings page stacks below `lg`). Default "md". */
  collapseBelow?: "md" | "lg";
  className?: string;
}): ReactNode {
  const shown = shownRailGroups<SectionRailItem<T>, SectionRailGroup<T>>(groups);
  const [railVis, chipVis] = COLLAPSE[collapseBelow];
  return (
    <div className={clsx("min-w-0", className)}>
      <nav aria-label={ariaLabel} className={clsx(railVis, "flex-col gap-4")}>
        {shown.map((g) => (
          <div key={g.id}>
            {g.label && (
              <div className="mb-1.5 px-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-fg-3">
                {g.label}
              </div>
            )}
            <div className="flex flex-col gap-0.5">
              {g.items.map((s) => {
                const link = isRailLink(s);
                const active = !link && s.id === value;
                // A link row with no glyph of its own wears the arrow in the
                // glyph slot (so it lines up with its neighbours); one with a
                // glyph gets the arrow after the label. Never both.
                const glyph = s.icon ?? (link ? ArrowUpRight : undefined);
                return (
                  <RailRow
                    key={s.id}
                    item={s}
                    active={active}
                    onChange={onChange}
                    className={clsx(
                      "flex items-center gap-2 rounded-2 px-2 py-1.5 text-left text-[12px] transition-colors focus:outline-none focus-visible:ring-1 focus-visible:ring-accent-ring",
                      active
                        ? "bg-bg-3 text-fg-0"
                        : s.tone === "danger"
                          ? "text-danger hover:bg-bg-2"
                          : "text-fg-2 hover:bg-bg-2 hover:text-fg-1",
                    )}
                  >
                    {glyph && (
                      <span
                        className={clsx(
                          "grid h-5 w-5 shrink-0 place-items-center rounded-1",
                          active ? "bg-bg-4 text-accent" : "bg-bg-2 text-fg-3",
                        )}
                        aria-hidden
                      >
                        <Icon icon={glyph} size={13} />
                      </span>
                    )}
                    <span className="min-w-0 flex-1 truncate">{s.label}</span>
                    {s.trailing}
                    {link && glyph !== ArrowUpRight && (
                      <Icon icon={ArrowUpRight} size="xs" aria-hidden className="shrink-0 text-fg-4" />
                    )}
                  </RailRow>
                );
              })}
            </div>
          </div>
        ))}
      </nav>
      <SectionChipRow
        groups={shown}
        value={value}
        onChange={onChange}
        ariaLabel={ariaLabel}
        className={chipVis}
      />
    </div>
  );
}

// One row or chip: a real <a> for an href link row, a button otherwise.
function RailRow<T extends string>({
  item,
  active,
  onChange,
  className,
  children,
}: {
  item: SectionRailItem<T>;
  active: boolean;
  onChange: (id: T) => void;
  className: string;
  children: ReactNode;
}) {
  if (item.href != null) {
    return (
      <a
        href={item.href}
        data-rail-link=""
        onClick={(e: MouseEvent<HTMLAnchorElement>) => {
          // Modified and middle clicks follow the href (new tab, new window).
          if (!interceptLinkClick(e)) return;
          e.preventDefault();
          if (item.onSelect) item.onSelect();
          else onChange(item.id);
        }}
        className={className}
      >
        {children}
      </a>
    );
  }
  return (
    <button
      type="button"
      aria-current={active ? "true" : undefined}
      data-rail-link={item.onSelect ? "" : undefined}
      onClick={() => (item.onSelect ? item.onSelect() : onChange(item.id))}
      className={className}
    >
      {children}
    </button>
  );
}

// The below-breakpoint form: one scrolling chip row, active chip kept in view.
function SectionChipRow<T extends string>({
  groups,
  value,
  onChange,
  ariaLabel,
  className,
}: {
  groups: SectionRailGroup<T>[];
  value: T;
  onChange: (id: T) => void;
  ariaLabel: string;
  className: string;
}) {
  const rowRef = useRef<HTMLDivElement>(null);
  const hasValue = railItems(groups).some((i) => i.id === value && !isRailLink(i));
  const { maskStyle, endPad } = useStripScroll(rowRef, hasValue ? value : null, (row) =>
    row.querySelector<HTMLElement>('[aria-current="true"]'),
  );
  return (
    <nav aria-label={ariaLabel} className={clsx(className, "min-w-0")}>
      <div
        ref={rowRef}
        style={maskStyle}
        className="relative flex items-center gap-1.5 overflow-x-auto pb-1"
      >
        {groups.map((g, gi) => (
          <div key={g.id} className="flex shrink-0 items-center gap-1.5">
            {gi > 0 && <span aria-hidden className="mx-0.5 h-4 w-px shrink-0 bg-line-2" />}
            {g.items.map((s) => {
              const link = isRailLink(s);
              const active = !link && s.id === value;
              return (
                <RailRow
                  key={s.id}
                  item={s}
                  active={active}
                  onChange={onChange}
                  className={clsx(
                    "inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full border px-2.5 py-1 text-[11.5px] transition-colors focus:outline-none focus-visible:ring-1 focus-visible:ring-accent-ring",
                    active
                      ? "border-accent/50 bg-accent-soft text-accent"
                      : s.tone === "danger"
                        ? "border-danger/40 text-danger hover:border-danger/60"
                        : "border-line-2 text-fg-2 hover:border-line-3 hover:text-fg-0",
                  )}
                >
                  {s.icon && <Icon icon={s.icon} size="xs" className="shrink-0" />}
                  {s.label}
                  {link && s.icon !== ArrowUpRight && (
                    <Icon icon={ArrowUpRight} size="xs" aria-hidden className="shrink-0 text-fg-4" />
                  )}
                </RailRow>
              );
            })}
          </div>
        ))}
        {endPad && <StripEndPad />}
      </div>
    </nav>
  );
}
