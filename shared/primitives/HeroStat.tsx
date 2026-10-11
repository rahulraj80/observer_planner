import clsx from "clsx";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { Sparkline, hasSparkline } from "./Sparkline";
import { StatDelta, type StatDeltaValue } from "./StatDelta";
import { useHelpSlot } from "./helpSlot";
import { AnimatedValue, Aurora, staleClass } from "./Motion";

// HeroStat — wide hero KPI tile for page-leading metrics like
// Compression's "Total compression savings" and Discovery's
// "Estimated waste". Layout intent matches
// `design/page-compression.jsx` + `design/page-discovery.jsx`: 2-3×
// the width of a StatCard, beefier value font, multi-line sub-text,
// saturated radial gradient. Designed to sit alongside a strip of
// regular StatCards in the same row.

export type HeroStatVariant = "accent" | "danger" | "warn";

/** The hero value's size whenever it fits (and for a node value). */
export const HERO_VALUE_PX = 44;
// Average advance of a bold glyph (digits are the widest) in em, so a value
// of n characters is about n * HERO_GLYPH_EM * font-size wide.
const HERO_GLYPH_EM = 0.62;

/**
 * heroValueFontSize sizes the value to its TILE rather than a fixed 44 px:
 * `min(44px, <k>cqw)`, where cqw is 1% of the value row's width (the row is
 * an inline-size container) and k makes `len` characters fill it. A 2-column
 * StatStrip at 375 gives a tile about 130 px of content, where a fixed 44 px
 * clipped even a 7-letter word ("observe"); a full-width hero keeps 44 px.
 * A trailing `unit` reserves about two more characters. Returns undefined
 * for a value with no text length (a node, e.g. web2's <Money>), which keeps
 * the 44 px class. A browser without container units drops the declaration
 * and falls back to that class too.
 */
export function heroValueFontSize(value: ReactNode, hasUnit = false): string | undefined {
  const len = typeof value === "string" || typeof value === "number" ? String(value).length : 0;
  if (len === 0) return undefined;
  const chars = len + (hasUnit ? 2 : 0);
  const cqw = Math.floor((100 / (chars * HERO_GLYPH_EM)) * 100) / 100;
  return `min(${HERO_VALUE_PX}px, ${cqw}cqw)`;
}

export type HeroStatProps = {
  label: string;
  value: ReactNode;
  unit?: ReactNode;
  sub?: ReactNode;
  variant?: HeroStatVariant;
  helpId?: string;
  icon?: ReactNode;
  cornerPill?: ReactNode;
  spark?: number[];
  sparkColor?: string;
  loading?: boolean;
  /** Dim while the value belongs to the previous filter. */
  stale?: boolean;
  /** Paint the drifting brand aurora behind the tile (a page's lead KPI). */
  aurora?: boolean;
  /**
   * "corner" (default): the small trend at the bottom right. "wide": a
   * full-width trend band along the bottom that draws itself in - for a
   * large lead tile with room to spare.
   */
  sparkMode?: "corner" | "wide";
  /**
   * Period-over-period delta, rendered like StatCard's (cost-aware: up =
   * danger, down = success): a signed fraction (0.12 = +12%) or a guarded
   * delta (web2 `format.computeDelta`).
   */
  delta?: StatDeltaValue;
  /** Text after the delta when `deltaPrior` is absent. */
  deltaLabel?: string;
  /** Concrete prior-period value (`vs prior $6,073.08`); wins over deltaLabel. */
  deltaPrior?: ReactNode;
  /** When set, the tile is a <Link> to this route and lifts on hover. */
  linkTo?: string;
  /** Accessible name for the link (defaults to the tile's text). */
  linkLabel?: string;
  className?: string;
};

export function HeroStat({
  label,
  value,
  unit,
  sub,
  variant = "accent",
  helpId,
  icon,
  cornerPill,
  spark,
  sparkColor,
  loading,
  stale,
  aurora,
  sparkMode = "corner",
  delta,
  deltaLabel,
  deltaPrior,
  linkTo,
  linkLabel,
  className,
}: HeroStatProps) {
  const renderHelp = useHelpSlot();
  const valueColor =
    variant === "danger"
      ? "text-danger"
      : variant === "warn"
        ? "text-warn"
        : "text-accent";
  const borderColor =
    variant === "danger"
      ? "border-danger/40"
      : variant === "warn"
        ? "border-warn/40"
        : "border-accent/40";
  const gradient =
    variant === "danger"
      ? "radial-gradient(circle at 100% 0%, color-mix(in srgb, var(--danger-soft) 92%, transparent), transparent 58%)"
      : variant === "warn"
        ? "radial-gradient(circle at 100% 0%, color-mix(in srgb, var(--warn-soft) 92%, transparent), transparent 58%)"
        : "radial-gradient(circle at 100% 0%, color-mix(in srgb, var(--accent-soft) 92%, transparent), transparent 58%)";
  const defaultSparkColor =
    sparkColor ??
    (variant === "danger"
      ? "var(--danger)"
      : variant === "warn"
        ? "var(--warn)"
        : "var(--accent)");
  const rootClass = clsx(
    "relative flex min-h-[140px] flex-col overflow-hidden rounded-3 border bg-bg-2 px-5 py-4",
    borderColor,
    linkTo && "sb-lift",
    staleClass(stale),
    className,
  );
  const hasDelta = delta != null || deltaPrior != null;
  const drawsSpark = spark != null && hasSparkline(spark);
  // The sub / delta row keeps clear of the CORNER sparkline only when one is
  // actually drawn; a tile without one gives its sub the full width.
  const reserveCorner = drawsSpark && sparkMode === "corner";
  const inner = (
    <>
      {aurora && <Aurora />}
      <span
        aria-hidden
        className="pointer-events-none absolute inset-0"
        style={{ background: gradient }}
      />
      <div
        className={clsx(
          "relative flex min-h-0 flex-1 flex-col",
          // Theme-aware shimmer (motion.css), not Tailwind's opacity pulse.
          loading && "animate-shimmer overflow-hidden",
        )}
      >
        <div className="flex items-start justify-between gap-2">
          <div className="flex items-center gap-1.5 text-[10.5px] font-semibold uppercase tracking-[0.08em] text-fg-3">
            {icon && <span className="text-fg-3">{icon}</span>}
            {label}
            {helpId && renderHelp(helpId)}
            {loading && (
              <span
                aria-label="loading"
                className="ml-0.5 inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-accent"
              />
            )}
          </div>
          {cornerPill && <div className="shrink-0">{cornerPill}</div>}
        </div>
        {/* An inline-size container, so the value can size to the tile
            (heroValueFontSize) instead of clipping in a narrow one. */}
        <div className="mt-2 flex min-w-0 items-baseline gap-2" style={{ containerType: "inline-size" }}>
          <span
            className={clsx(
              "min-w-0 text-[44px] font-bold leading-[1.02] tracking-[-0.03em]",
              valueColor,
            )}
            style={{ fontSize: heroValueFontSize(value, unit != null) }}
          >
            <AnimatedValue value={value} />
          </span>
          {unit && (
            <span className="text-[18px] font-medium text-fg-2">{unit}</span>
          )}
        </div>
        {hasDelta ? (
          // The delta row: StatCard's order (delta, prior or label, sub).
          <div
            className={clsx(
              "mt-2 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[12px] leading-snug text-fg-3",
              reserveCorner && "pr-[100px]",
            )}
          >
            <StatDelta delta={delta} deltaLabel={deltaLabel} deltaPrior={deltaPrior} />
            {sub && <span>{sub}</span>}
          </div>
        ) : (
          sub && (
            <div
              className={clsx(
                "mt-2 text-[12px] leading-snug text-fg-3",
                reserveCorner && "pr-[100px]",
              )}
            >
              {sub}
            </div>
          )
        )}
      </div>
      {spark && drawsSpark && sparkMode === "wide" && (
        <div aria-hidden className="pointer-events-none relative -mx-5 -mb-4 mt-auto pt-4">
          <Sparkline
            data={spark}
            color={defaultSparkColor}
            width={400}
            height={72}
            strokeWidth={1.8}
            stretch
            reveal
          />
        </div>
      )}
      {spark && reserveCorner && (
        <div
          aria-hidden
          className="pointer-events-none absolute bottom-3 right-4 opacity-[0.85]"
        >
          <Sparkline
            data={spark}
            color={defaultSparkColor}
            width={88}
            height={32}
          />
        </div>
      )}
    </>
  );
  if (linkTo) {
    return (
      <Link to={linkTo} aria-label={linkLabel} className={rootClass}>
        {inner}
      </Link>
    );
  }
  return <div className={rootClass}>{inner}</div>;
}
