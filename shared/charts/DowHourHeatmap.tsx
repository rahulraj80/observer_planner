import { memo, useMemo, useState, type ReactElement } from "react";
import { dollarsShown, fmtInt, fmtUSD } from "../lib/format";
import type { AnalysisCostByDowHour } from "../lib/types";
import { Tooltip } from "../primitives";
import { ScaleLegend } from "./ScaleLegend";

// DowHourHeatmap — 7×24 grid showing cost concentration across
// day-of-week (rows) × hour-of-day (cols), matching the design's
// "When you spend" panel (`design/page-analysis.jsx`). Replaces the
// older 1D HourHeatmap once /api/analysis/cost-by-dow-hour is wired.
//
// Cells render with intensity = log(cost) / log(max) so a hot
// hour-cluster (e.g. Tue/Wed 14:00–17:00) reads at-a-glance while
// quieter cells stay visible rather than collapsing to bg.
// The wire shape is a prop (`metric`): without one the cells are the node's
// cost cells and the heat quantity is cost, or the turn count in the token
// usage view (format.ts cost affordances; the cells carry no token split),
// read at render time after the display mode is set. web2 passes its own
// metric for the org's action-count cells.
type Pos = { dow: number; hour: number };
type CostCell = AnalysisCostByDowHour["cells"][number];

/** HeatMetric reads one cell of shape C. */
export type HeatMetric<C extends Pos> = {
  value: (c: C) => number;
  fmt: (n: number) => string;
  /** An extra readout after the figure (e.g. the turn count), or null. */
  suffix?: (c: C) => string | null;
  caption: string;
  scaleLabel: string;
};

const turns = (n: number) => `${n} turn${n === 1 ? "" : "s"}`;

/** costHeatMetric is the default metric for the node's cost cells. */
export function costHeatMetric(): HeatMetric<CostCell> {
  return dollarsShown()
    ? {
        value: (c) => c.cost_usd,
        fmt: (n) => fmtUSD(n),
        suffix: (c) => (c.turn_count > 0 ? turns(c.turn_count) : null),
        caption: "cost by hour of day × day of week",
        scaleLabel: "Cost colour scale, log",
      }
    : {
        value: (c) => c.turn_count,
        fmt: (n) => `${fmtInt(n)} turn${n === 1 ? "" : "s"}`,
        caption: "turns by hour of day × day of week",
        scaleLabel: "Turn count colour scale, log",
      };
}

type HeatmapProps<C extends Pos> = {
  cells: readonly C[];
  timezone?: string;
};

function DowHourHeatmapImpl<C extends Pos>({
  cells,
  timezone,
  metric: given,
}: HeatmapProps<C> & { metric?: HeatMetric<C> }) {
  // peak is the real top cell (the legend's honest high end); max floors
  // it at 1 only as the log-scale divisor.
  const metric = (given ?? costHeatMetric()) as unknown as HeatMetric<C>;
  const peak = useMemo(() => {
    let m = 0;
    for (const c of cells) if (metric.value(c) > m) m = metric.value(c);
    return m;
    // eslint-disable-next-line react-hooks/exhaustive-deps -- the metric only changes with the boot-time display mode
  }, [cells]);
  const max = Math.max(1, peak);

  const [hovered, setHovered] = useState<{ dow: number; hour: number } | null>(
    null,
  );

  // Day labels — Go's time.Weekday() encodes Sun=0..Sat=6; the design
  // mockup leads with Mon, so render in that order while preserving
  // the raw dow integer for cell lookup.
  const DAYS: { label: string; dow: number }[] = [
    { label: "Mon", dow: 1 },
    { label: "Tue", dow: 2 },
    { label: "Wed", dow: 3 },
    { label: "Thu", dow: 4 },
    { label: "Fri", dow: 5 },
    { label: "Sat", dow: 6 },
    { label: "Sun", dow: 0 },
  ];

  const lookup = useMemo(() => {
    const m = new Map<number, C>();
    for (const c of cells) m.set(c.dow * 24 + c.hour, c);
    return m;
  }, [cells]);

  const sel =
    hovered != null
      ? lookup.get(hovered.dow * 24 + hovered.hour) ?? null
      : null;

  return (
    <div className="flex flex-col gap-2">
      <div className="overflow-x-auto">
        <div className="inline-grid min-w-full grid-cols-[36px_minmax(0,1fr)] items-center gap-y-px">
          {DAYS.map(({ label, dow }) => (
            <Row
              key={dow}
              label={label}
              dow={dow}
              lookup={lookup}
              max={max}
              metric={metric}
              hovered={hovered}
              onHover={setHovered}
            />
          ))}
        </div>
      </div>

      <div className="grid grid-cols-[36px_minmax(0,1fr)] gap-2 text-[9.5px] text-fg-4">
        <span />
        <div className="grid grid-cols-24 gap-px">
          {Array.from({ length: 24 }).map((_, hour) => (
            <span
              key={hour}
              className="text-center font-mono tabular-nums"
              style={{ visibility: hour % 3 === 0 ? "visible" : "hidden" }}
            >
              {pad(hour)}
            </span>
          ))}
        </div>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 text-[10.5px] text-fg-3">
        <div className="flex items-center gap-2">
          <span className="font-mono uppercase tracking-[0.06em]">
            {metric.caption} · {timezone ?? "UTC"}
          </span>
        </div>
        <ScaleLegend
          colorAt={cellColor}
          low={metric.fmt(0)}
          high={metric.fmt(peak)}
          label={metric.scaleLabel}
        />
      </div>

      {/* Fixed-height readout slot — operator complaint: the prior
          implementation toggled between a paragraph and a panel,
          which jumped the surrounding block's height as the cursor
          moved in / out. Reserved a constant 32px row with default
          "hover a cell" copy that swaps in-place to the data row.
          Same surface dims either way. */}
      <div className="flex h-[32px] items-center rounded-2 border border-line-1 bg-bg-3/40 px-3 text-[11px]">
        {sel ? (
          <>
            <span className="font-mono text-fg-3">
              From: {dayName(sel.dow)} {pad(sel.hour)}:00-{pad(sel.hour + 1)}:00{" "}
              {timezone ?? "UTC"}
            </span>
            <span className="px-1 text-fg-4">·</span>
            <span className="font-semibold tabular-nums text-fg-0">
              {metric.fmt(metric.value(sel))}
            </span>
            {metric.suffix?.(sel) && (
              <span className="ml-2 text-fg-3">· {metric.suffix(sel)}</span>
            )}
          </>
        ) : (
          <span className="text-fg-4">Hover a cell for the hour breakdown.</span>
        )}
      </div>
    </div>
  );
}

/**
 * DowHourHeatmap draws the 7 x 24 day-of-week by hour grid. Without
 * `metric` the cells are the node's cost cells; with one, any
 * `{ dow, hour }` shape the metric reads.
 */
export const DowHourHeatmap = memo(DowHourHeatmapImpl) as unknown as {
  (props: HeatmapProps<CostCell> & { metric?: HeatMetric<CostCell> }): ReactElement;
  <C extends Pos>(props: HeatmapProps<C> & { metric: HeatMetric<C> }): ReactElement;
};

function Row<C extends Pos>({
  label,
  dow,
  lookup,
  max,
  metric,
  hovered,
  onHover,
}: {
  label: string;
  dow: number;
  lookup: Map<number, C>;
  max: number;
  metric: HeatMetric<C>;
  hovered: { dow: number; hour: number } | null;
  onHover: (h: { dow: number; hour: number } | null) => void;
}) {
  return (
    <>
      <span className="pr-2 text-right font-mono text-[9.5px] uppercase tracking-[0.06em] text-fg-3">
        {label}
      </span>
      <div className="grid grid-cols-24 gap-px">
        {Array.from({ length: 24 }).map((_, hour) => {
          const cell = lookup.get(dow * 24 + hour);
          const value = cell ? metric.value(cell) : 0;
          // Log scale — linear scale washes out off-peak hours in a
          // window dominated by one heavy day. log1p keeps the tail
          // visible while still letting the peak stand out.
          const intensity =
            value > 0 ? Math.min(1, Math.log1p(value) / Math.log1p(max)) : 0;
          const isHovered = hovered?.dow === dow && hovered?.hour === hour;
          return (
            <Tooltip
              key={hour}
              content={`${dayName(dow)} ${pad(hour)}:00 · ${metric.fmt(value)}${
                cell && metric.suffix?.(cell) ? ` · ${metric.suffix(cell)}` : ""
              }`}
            >
              <button
                type="button"
                onMouseEnter={() => onHover({ dow, hour })}
                onMouseLeave={() => onHover(null)}
                onFocus={() => onHover({ dow, hour })}
                onBlur={() => onHover(null)}
                className="aspect-square w-full rounded-[2px] transition-all"
                style={{
                  background: cellColor(intensity),
                  outline: isHovered ? "1px solid var(--accent)" : "none",
                  outlineOffset: isHovered ? "1px" : undefined,
                }}
              />
            </Tooltip>
          );
        })}
      </div>
    </>
  );
}

// cellColor is the cell ramp (accent over bg-3); the ScaleLegend samples
// the same function so the legend and the cells cannot drift.
function cellColor(intensity: number): string {
  return intensity > 0
    ? `color-mix(in srgb, var(--accent) ${(14 + intensity * 76).toFixed(0)}%, var(--bg-3))`
    : "var(--bg-3)";
}

function pad(n: number): string {
  return n.toString().padStart(2, "0");
}

// Go's time.Weekday() order: Sun=0 .. Sat=6.
const DAY_NAMES = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"] as const;

function dayName(dow: number): string {
  return DAY_NAMES[dow] ?? String(dow);
}
