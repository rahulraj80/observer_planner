import { memo, type ReactElement } from "react";
import { fmtUSD } from "../lib/format";
import type { HourBucket } from "../lib/types";
import { Tooltip } from "../primitives";
import { ScaleLegend } from "./ScaleLegend";

// 24-bar "when it happens" chart (UTC hours). Hand-rolled so each bar can
// render its hour label + value tooltip without Recharts overhead.
// Color intensity scales with the measured value: hotter hours get warmer
// tones, and the scale legend under the bars (on by default; `legend={false}`
// for a caller that explains the ramp itself) maps a tint back to a range.
//
// The wire shape is a prop (`measure`), so one chart serves the node's cost
// buckets (the default, HOUR_COST_MEASURE) and the org's action counts (web2
// passes its own measure); the formatting a measure needs is the caller's.

/** HourMeasure reads one hour bucket of shape B. */
export type HourMeasure<B extends { hour: number }> = {
  /** The plotted quantity. */
  value: (b: B) => number;
  /** An empty bucket for an hour the wire omitted. */
  empty: (hour: number) => B;
  /** The readout after "HH:00 UTC · " in the tooltip and the peak note. */
  describe: (b: B) => string;
  /** Formats a legend end. */
  fmt: (v: number) => string;
  /** Accessible name of the colour scale. */
  scaleLabel: string;
};

/** HOUR_COST_MEASURE is the node's cost-per-hour shape (the default). */
export const HOUR_COST_MEASURE: HourMeasure<HourBucket> = {
  value: (b) => b.cost_usd,
  empty: (hour) => ({ hour, cost_usd: 0, turn_count: 0 }),
  describe: (b) => `${fmtUSD(b.cost_usd)} · ${b.turn_count} turns`,
  fmt: (v) => fmtUSD(v),
  scaleLabel: "Cost colour scale",
};

type HourBarsProps<B extends { hour: number }> = {
  buckets: readonly B[];
  legend?: boolean;
  /** Tallest bar in px (the chart is this plus the hour labels). */
  barHeight?: number;
};

function HourBarsImpl<B extends { hour: number }>({
  buckets,
  measure,
  legend = true,
  barHeight = 170,
}: HourBarsProps<B> & { measure?: HourMeasure<B> }) {
  const m = (measure ?? HOUR_COST_MEASURE) as unknown as HourMeasure<B>;
  // Recharts could do this but it's a perfect case for a tight
  // hand-rolled grid: 24 fixed columns + per-bar gradient.
  const filled = Array.from({ length: 24 }, (_, h) =>
    buckets.find((b) => b.hour === h) ?? m.empty(h),
  );
  // peak is the real top hour (the legend's honest high end); max floors
  // it at 1 only as the intensity divisor.
  const peakBucket = filled.reduce((a, b) => (m.value(b) > m.value(a) ? b : a), filled[0]);
  const peak = Math.max(0, m.value(peakBucket));
  const max = Math.max(1, peak);

  const bars = (
    <div className="flex items-end gap-[2px] px-1" style={{ height: `${barHeight + 30}px` }}>
      {filled.map((b) => {
        const intensity = m.value(b) / max;
        const h = Math.max(2, intensity * barHeight);
        return (
          <div
            key={b.hour}
            className="group flex flex-1 flex-col items-center gap-1"
          >
            <Tooltip content={`${pad(b.hour)}:00 UTC · ${m.describe(b)}`}>
              <div
                tabIndex={0}
                className="w-full cursor-help rounded-t-[2px] transition-opacity group-hover:opacity-90 focus:outline-none"
                style={{
                  height: `${h}px`,
                  background: hourTint(intensity),
                }}
              />
            </Tooltip>
            <span className="text-[9px] tabular-nums text-fg-3">
              {pad(b.hour)}
            </span>
          </div>
        );
      })}
    </div>
  );
  if (!legend) return bars;
  return (
    <div className="flex flex-col gap-2">
      {bars}
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 px-1 text-[10px] text-fg-3">
        <span className="tabular-nums">
          Hour of day (UTC)
          {peak > 0 && ` · peak ${pad(peakBucket.hour)}:00 · ${m.describe(peakBucket)}`}
        </span>
        <ScaleLegend
          colorAt={hourTint}
          stops={[0.1, 0.3, 0.5, 0.7, 0.9]}
          low={m.fmt(0)}
          high={m.fmt(peak)}
          label={m.scaleLabel}
        />
      </div>
    </div>
  );
}

/**
 * HourBars draws 24 UTC hour bars. Without `measure` the buckets are the
 * node's cost shape; with one, any `{ hour }` shape the measure reads.
 */
export const HourBars = memo(HourBarsImpl) as unknown as {
  (props: HourBarsProps<HourBucket> & { measure?: HourMeasure<HourBucket> }): ReactElement;
  <B extends { hour: number }>(props: HourBarsProps<B> & { measure: HourMeasure<B> }): ReactElement;
};

// TINT_STEPS: the bar colour for an intensity below each `upTo`, walked
// top-down (cool blue to warm orange; the same hues as --tok-net to
// --tok-write). A zero hour renders as the empty track.
const TINT_STEPS: readonly { upTo: number; color: string }[] = [
  { upTo: 0.2, color: "color-mix(in srgb, var(--tok-net) 35%, var(--bg-4))" },
  { upTo: 0.4, color: "color-mix(in srgb, var(--tok-net) 60%, transparent)" },
  { upTo: 0.6, color: "color-mix(in srgb, var(--accent) 70%, transparent)" },
  { upTo: 0.8, color: "color-mix(in srgb, var(--warn) 75%, transparent)" },
  { upTo: Infinity, color: "var(--warn)" },
];

function hourTint(t: number): string {
  if (t <= 0) return "var(--bg-4)";
  return TINT_STEPS.find((s) => t < s.upTo)?.color ?? "var(--warn)";
}

function pad(n: number): string {
  return String(n).padStart(2, "0");
}
