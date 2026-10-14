import clsx from "clsx";
import type { ReactNode } from "react";
import { CircleCheck, CircleDashed, CircleHelp, CircleSlash, type LucideIcon } from "lucide-react";
import { Pill } from "./Pill";
import { COVERAGE_TONE, type CoverageStatus } from "./copyModel";

// CoverageBadge: a compact "is this enforced here?" status (docs/
// app-design-system.md "Coverage badge"). Built for the Budgets "Enforced on"
// column and reusable on Fleet state and Node components: an enum-driven pill
// (the status picks the tone and glyph, never the page), an optional one-line
// reason under it ("observe mode on pi"), and an optional detail tooltip on
// the pill for the full rule.
//
// The caller passes the label text from its vocab ("Machine: yes",
// coverageCountLabel(1, 3) + " members", "Not enforced"); the badge never
// renders a raw enum. Only `partial` and `none` are coloured warn and
// danger, so a covered row stays calm.
export type CoverageBadgeProps = {
  status: CoverageStatus;
  /** The pill text, sentence case, from the caller's vocab. */
  children: ReactNode;
  /** One short line under the pill: the single reason that matters. */
  reason?: ReactNode;
  /** Tooltip on the pill: the full rule or the per-member breakdown. */
  detail?: ReactNode;
  className?: string;
};

const GLYPH: Record<CoverageStatus, LucideIcon> = {
  full: CircleCheck,
  partial: CircleDashed,
  none: CircleSlash,
  unknown: CircleHelp,
};

export function CoverageBadge({ status, children, reason, detail, className }: CoverageBadgeProps) {
  return (
    <span className={clsx("inline-flex flex-col items-start gap-0.5", className)}>
      <Pill variant={COVERAGE_TONE[status]} icon={GLYPH[status]} title={detail} case="none">
        {children}
      </Pill>
      {reason && <span className="text-[11px] leading-snug text-fg-3">{reason}</span>}
    </span>
  );
}
