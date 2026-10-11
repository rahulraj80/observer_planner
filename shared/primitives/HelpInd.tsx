import clsx from "clsx";
import { CircleHelp } from "lucide-react";
import { Icon } from "./Icon";
import { Tooltip } from "./Tooltip";

// HelpInd is the inline indicator a column header / tile / chart renders to
// flag "there's help for this entity." Hovering shows how to get it; clicking
// opens the app's help drawer scrolled to the entry. Pure: it only carries
// `data-help-id`; each app registers ONE delegated click listener on
// `[data-help-id]` (web App.tsx, web2 Layout.tsx) that opens its own drawer,
// so this file never imports a help registry (no registry in the shell chunk)
// and shared stays free of any app's help system (see helpSlot.tsx).
//
// Imports Icon / Tooltip by module, not through the primitives barrel, so an
// eager primitive (StatCard) can render it without an import cycle.
export function HelpInd({
  id,
  className,
}: {
  id: string;
  className?: string;
}) {
  return (
    <Tooltip
      content={
        <span className="block">
          Click for help <span className="text-fg-3">·</span> press{" "}
          <kbd>?</kbd> for the full drawer
        </span>
      }
      side="top"
      maxWidth={240}
    >
      <button
        type="button"
        data-help-id={id}
        aria-label="Show help"
        className={clsx(
          "ml-1 inline-flex h-3.5 w-3.5 items-center justify-center rounded-pill text-fg-3 hover:text-accent focus:text-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
          className,
        )}
      >
        <Icon icon={CircleHelp} size={13} />
      </button>
    </Tooltip>
  );
}

// TitleWithHelp pairs a text title with an inline HelpInd. Designed to be
// passed as ChartShell's / Card's `title` prop (ReactNode-typed).
export function TitleWithHelp({
  text,
  helpId,
}: {
  text: string;
  helpId: string;
}) {
  return (
    <span className="inline-flex items-center">
      {text}
      <HelpInd id={helpId} />
    </span>
  );
}
