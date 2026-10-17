import clsx from "clsx";
import type { ReactNode } from "react";
import { HelpTooltip } from "./HelpTooltip";
import { InfoDisclosure } from "./InfoDisclosure";

// SettingRow — one labelled setting: a label (plus optional help and
// badges) beside or above its control, with an optional status line under
// the control. Generalised from the Settings forms' `FieldRow` / `Field`
// so the config surfaces stop each inventing their own grid.
//
// `layout`:
//   "grid"  (default) label column + control column at >= lg, stacked on
//           phones. `help` sits under the LABEL, as the config forms do.
//           Unchanged since before the split layout; keep it for short
//           help on dense settings forms.
//   "stack" label above the control, `help` under the CONTROL — the shape
//           the Intelligence panel's small fields use.
//   "split" (design review 2026-10-13, docs/app-design-system.md "Setting
//           rows") the row reads left to right: a sentence-case label and
//           ONE line of description on the left, the control and ONE line
//           saying what the current choice does (`consequence`) on the
//           right. Long help never sits in a narrow column: it goes behind
//           `learnMore` (an InfoDisclosure) and a config key or file path
//           goes in `technical` (an (i) tooltip). The two sides wrap onto
//           separate lines when the row is narrow (flex-wrap with a basis,
//           so it works inside a two-column card grid, not only at a page
//           breakpoint). In split, `help` is accepted as the description
//           when `description` is not given, so a grid row converts by
//           changing `layout` alone.
//
// The label-column width is an enum, not a free number, because Tailwind
// cannot see a class assembled from a variable.

export type SettingRowProps = {
  label: ReactNode;
  /** grid: under the label. stack: under the control. split: the description when `description` is unset. */
  help?: ReactNode;
  /** split: one line under the label (copyModel COPY_LIMITS.settingDescription). */
  description?: ReactNode;
  /** split: one line under the control saying what the current choice does. */
  consequence?: ReactNode;
  /** split / grid: technical details (a config key, file path, enum) behind an (i) tooltip beside the label. */
  technical?: ReactNode;
  /** split / grid: the longer explanation, behind a "Learn more" disclosure. */
  learnMore?: ReactNode;
  /** The setting's name as plain text, for the (i) button's accessible name. */
  subject?: string;
  /** Rendered under the control: effective value, validation, "saved". */
  status?: ReactNode;
  layout?: "grid" | "stack" | "split";
  /** Label-column width in the grid layout. */
  width?: "sm" | "md";
  /** split: "wide" gives the control about 55% of the row; "compact" keeps a small control (a Select) at the right edge. */
  control?: "wide" | "compact";
  /** Tints the row, e.g. for an unsaved edit. */
  highlight?: boolean;
  className?: string;
  children: ReactNode;
};

const WIDTH_CLASS: Record<"sm" | "md", string> = {
  sm: "lg:grid-cols-[180px_minmax(0,1fr)]",
  md: "lg:grid-cols-[220px_minmax(0,1fr)]",
};

// LearnMore renders the row's disclosure. When a row has both, the technical
// details ride inside the disclosure (under a caption) instead of a second
// affordance beside the label, so a row carries at most one way to read more.
function LearnMore({ technical, learnMore }: Pick<SettingRowProps, "technical" | "learnMore">) {
  if (learnMore === undefined) return null;
  return (
    <InfoDisclosure className="mt-1">
      {learnMore}
      {technical !== undefined && (
        <span className="mt-1.5 block">
          <span className="block text-[10px] font-semibold text-fg-3">Technical details</span>
          <span className="block break-words font-mono text-[11px] text-fg-3">{technical}</span>
        </span>
      )}
    </InfoDisclosure>
  );
}

export function SettingRow({
  label,
  help,
  description,
  consequence,
  technical,
  learnMore,
  subject,
  status,
  layout = "grid",
  width = "sm",
  control = "wide",
  highlight,
  className,
  children,
}: SettingRowProps) {
  const tip =
    technical !== undefined && learnMore === undefined ? (
      <HelpTooltip technical content={technical} subject={subject} />
    ) : null;

  if (layout === "stack") {
    return (
      <div className={className}>
        <div className="mb-1 text-[10.5px] font-semibold uppercase tracking-[0.06em] text-fg-3">
          {label}
        </div>
        {children}
        {help !== undefined && (
          <div className="mt-1 text-[11px] leading-snug text-fg-3">{help}</div>
        )}
        {status !== undefined && <div className="mt-1">{status}</div>}
      </div>
    );
  }

  if (layout === "split") {
    const desc = description ?? help;
    const compact = control === "compact";
    return (
      <div
        className={clsx(
          "flex flex-wrap items-start gap-x-6 gap-y-2",
          highlight && "rounded-2 bg-accent/5",
          className,
        )}
      >
        <div className={clsx("min-w-0", compact ? "flex-[1_1_12rem]" : "flex-[1_1_16rem]")}>
          <div className="flex items-center gap-1 text-[13px] font-medium leading-snug text-fg-1">
            <span className="min-w-0">{label}</span>
            {tip}
          </div>
          {desc !== undefined && (
            <div className="mt-0.5 text-[12px] leading-snug text-fg-3">{desc}</div>
          )}
          <LearnMore technical={technical} learnMore={learnMore} />
        </div>
        <div
          className={clsx(
            "min-w-0",
            compact ? "ml-auto flex max-w-full shrink-0 flex-col items-end" : "flex-[1.2_1_18rem]",
          )}
        >
          {children}
          {consequence !== undefined && (
            <div className="mt-1 text-[11.5px] leading-snug text-fg-3">{consequence}</div>
          )}
          {status !== undefined && <div className="mt-1">{status}</div>}
        </div>
      </div>
    );
  }

  return (
    <div
      className={clsx(
        "grid grid-cols-1 gap-1.5 lg:items-start lg:gap-4",
        WIDTH_CLASS[width],
        highlight && "rounded-2 bg-accent/5",
        className,
      )}
    >
      <div className="lg:pt-1.5">
        {tip ? (
          <div className="flex items-center gap-1 text-[11px] font-semibold uppercase tracking-[0.06em] text-fg-2">
            <span className="min-w-0">{label}</span>
            {tip}
          </div>
        ) : (
          <div className="text-[11px] font-semibold uppercase tracking-[0.06em] text-fg-2">
            {label}
          </div>
        )}
        {help !== undefined && (
          <div className="mt-1 text-[11px] leading-snug text-fg-3">{help}</div>
        )}
        <LearnMore technical={technical} learnMore={learnMore} />
      </div>
      <div className="min-w-0">
        {children}
        {status !== undefined && <div className="mt-1">{status}</div>}
      </div>
    </div>
  );
}
