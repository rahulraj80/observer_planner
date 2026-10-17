import clsx from "clsx";
import type { ReactNode } from "react";
import { Info } from "lucide-react";
import { Icon } from "./Icon";
import { Tooltip } from "./Tooltip";

// HelpTooltip: the field-level help pattern (docs/app-design-system.md
// "Field help"). A small (i) button beside a label that shows one or two
// sentences on hover AND on keyboard focus, so help is reachable without a
// mouse. It is a real <button> in the tab order with an accessible name
// ("About <subject>"), and the Tooltip wires aria-describedby while open.
//
// `helpId` makes the same button open the app's help drawer at that entry:
// it only carries `data-help-id`, which each app's one delegated listener
// handles (the HelpInd contract), so this file never imports a help
// registry. Without it the click just keeps the tooltip open on focus.
//
// `technical` styles the content as technical details (mono, muted) for the
// one place a config key, file path or enum value may appear in the UI.
//
// Tooltip content must not be interactive (no links or buttons inside); for
// a longer explanation use InfoDisclosure or the help drawer.
export type HelpTooltipProps = {
  /** The help text: one or two sentences. */
  content: ReactNode;
  /** What the help is about, for the button's accessible name ("About <subject>"). */
  subject?: string;
  /** Opens the app's help drawer at this entry on click. */
  helpId?: string;
  /** Render the content as technical details (mono, under a "Technical details" caption). */
  technical?: boolean;
  side?: "top" | "bottom" | "left" | "right";
  maxWidth?: number;
  className?: string;
};

export function HelpTooltip({
  content,
  subject = "this setting",
  helpId,
  technical,
  side = "top",
  maxWidth = 320,
  className,
}: HelpTooltipProps) {
  const body = technical ? (
    <span className="block">
      <span className="mb-0.5 block text-[10px] font-semibold text-fg-3">Technical details</span>
      <span className="block break-words font-mono text-[11px] text-fg-1">{content}</span>
    </span>
  ) : (
    <span className="block">
      {content}
      {helpId && <span className="mt-1 block text-fg-3">Click for more in Help.</span>}
    </span>
  );
  return (
    <Tooltip content={body} side={side} maxWidth={maxWidth}>
      <button
        type="button"
        data-help-id={helpId}
        aria-label={technical ? `Technical details for ${subject}` : `About ${subject}`}
        className={clsx(
          "inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-pill text-fg-3 hover:text-accent focus:text-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
          helpId ? "cursor-pointer" : "cursor-help",
          className,
        )}
      >
        <Icon icon={Info} size={13} />
      </button>
    </Tooltip>
  );
}
