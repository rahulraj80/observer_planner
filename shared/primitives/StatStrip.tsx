import type { ReactNode } from "react";
import clsx from "clsx";
import { Stagger } from "./Motion";

// StatStrip: the summary strip of a tabbed page (docs/app-design-system.md
// "Page tabs"): the whole object's headline numbers, the same on every tab,
// between the PageHeader and the TabStrip. It is layout only and composes the
// existing StatCard (or StatCardSkeleton while loading); it replaces the
// hand-built `<Stagger className="grid grid-cols-2 gap-3 md:grid-cols-4">`
// grids pages repeat today.
//
// CONTENT-FREE ONLY. Anything that triggers an audited read belongs in a tab,
// never in the strip: the strip renders on every tab, including the default.
//
// Two columns below `md` (a phone), `cols` from `md` up. Five and six step
// through three columns first so a 768-1024 px window does not crush values.

const COLS: Record<2 | 3 | 4 | 5 | 6, string> = {
  2: "md:grid-cols-2",
  3: "md:grid-cols-3",
  4: "md:grid-cols-4",
  5: "md:grid-cols-3 lg:grid-cols-5",
  6: "md:grid-cols-3 xl:grid-cols-6",
};

export function StatStrip({
  cols = 4,
  ariaLabel,
  className,
  children,
}: {
  /** Columns from `md` up (the number of StatCards, usually). Default 4. */
  cols?: 2 | 3 | 4 | 5 | 6;
  /** Accessible name for the strip's region (e.g. "Developer summary"). */
  ariaLabel?: string;
  /** Classes for the outer box; REPLACES the default `mb-4` (the gap above the TabStrip). */
  className?: string;
  /** StatCard / StatCardSkeleton tiles. */
  children: ReactNode;
}): ReactNode {
  return (
    <section aria-label={ariaLabel} className={className ?? "mb-4"}>
      <Stagger className={clsx("grid grid-cols-2 gap-3", COLS[cols])}>{children}</Stagger>
    </section>
  );
}
