import clsx from "clsx";
import { useEffect, useState } from "react";
import type { LucideIcon } from "lucide-react";
import { Icon } from "./Icon";

// SectionNav: a sticky row of chips that jump to sections of a long page and
// highlight the one in view. For pages that grew into one long column
// (Analysis). Pure presentational: the page places an element with each
// item's id where the section starts; this component only observes those.

export type SectionNavItem = {
  id: string;
  label: string;
  /** Optional glyph before the label (the section card's own icon). */
  icon?: LucideIcon;
  /** "danger" tints a destructive section's chip (e.g. delete my data). */
  tone?: "danger";
};

// Chip colours: [inactive, active] per tone.
const CHIP_TONE = {
  default: [
    "border-line-2 text-fg-2 hover:border-line-3 hover:text-fg-0",
    "border-accent/50 bg-accent-soft text-accent",
  ],
  danger: [
    "border-danger/40 text-danger hover:border-danger/60",
    "border-danger/60 bg-danger-soft text-danger",
  ],
} as const;

/**
 * The page gutter the sticky row bleeds into, so its underline and backdrop
 * reach the edges of the app's <main> while the chips stay aligned with the
 * page content. Pick the one that matches the app shell's own padding.
 */
export type SectionNavGutter = "default" | "org" | "none";

// Literal class strings per gutter (Tailwind cannot see built names).
//   default: px-4 / sm:px-6 - the node dashboard (web) and the portal (webcloud)
//   org:     px-3 / sm:px-6 - the org dashboard's <main> (web2 Layout.tsx)
//   none:    no bleed, for a row inside a card or a padded container
export const SECTION_NAV_GUTTER: Record<SectionNavGutter, string> = {
  default: "-mx-4 px-4 sm:-mx-6 sm:px-6",
  org: "-mx-3 px-3 sm:-mx-6 sm:px-6",
  none: "",
};

export function SectionNav({
  items,
  gutter = "default",
  className,
}: {
  items: SectionNavItem[];
  /** The app gutter to bleed into. Default "default" (px-4 / sm:px-6); web2
   *  pages pass "org" (px-3 / sm:px-6). */
  gutter?: SectionNavGutter;
  className?: string;
}) {
  const [active, setActive] = useState(items[0]?.id ?? "");
  useEffect(() => {
    if (typeof IntersectionObserver === "undefined") return;
    const els = items
      .map((i) => document.getElementById(i.id))
      .filter((e): e is HTMLElement => e != null);
    if (els.length === 0) return;
    const obs = new IntersectionObserver(
      (entries) => {
        const hit = entries
          .filter((e) => e.isIntersecting)
          .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)[0];
        if (hit) setActive(hit.target.id);
      },
      // A section is "current" once its anchor crosses the top third.
      { rootMargin: "0px 0px -66% 0px", threshold: 0 },
    );
    els.forEach((e) => obs.observe(e));
    return () => obs.disconnect();
  }, [items]);

  return (
    <nav
      aria-label="On this page"
      className={clsx(
        "sticky top-0 z-20 flex gap-1.5 overflow-x-auto border-b border-line-1 bg-bg-0/85 py-2 backdrop-blur",
        SECTION_NAV_GUTTER[gutter],
        className,
      )}
    >
      {items.map((it) => (
        <a
          key={it.id}
          href={`#${it.id}`}
          aria-current={active === it.id ? "true" : undefined}
          onClick={(e) => {
            const el = document.getElementById(it.id);
            if (!el) return;
            e.preventDefault();
            setActive(it.id);
            el.scrollIntoView({ behavior: "smooth", block: "start" });
          }}
          className={clsx(
            "sb-press inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-pill border px-2.5 py-1 text-[11.5px] font-medium transition-colors duration-fast focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring",
            CHIP_TONE[it.tone ?? "default"][active === it.id ? 1 : 0],
          )}
        >
          {it.icon && <Icon icon={it.icon} size="xs" className="shrink-0" />}
          {it.label}
        </a>
      ))}
    </nav>
  );
}
