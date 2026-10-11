import clsx from "clsx";
import { Search } from "lucide-react";
import { Icon } from "./Icon";
import { Tooltip } from "./Tooltip";

// SearchChip is the filter row's button-chip that opens the Cmd/Ctrl-K
// command palette (design/shell.jsx:168-172). Promoted from the node
// dashboard's FilterBar so the org dashboard's filter row carries the same
// chip (T-452); the palette itself stays each app's own (each searches its
// own data). When the app keeps a query in effect, the chip shows it
// (truncated) so the viewer can see a search applies. Pure: the caller owns
// the palette and passes `onOpen`.
export function SearchChip({
  query = "",
  onOpen,
  hint = "search pages, sessions, actions",
  className,
}: {
  query?: string;
  onOpen: () => void;
  /** hint names what the palette searches, after "Open command palette". */
  hint?: string;
  className?: string;
}) {
  const trimmed = query.trim();
  return (
    <Tooltip
      content={
        <>
          Open command palette <kbd>⌘K</kbd> - {hint}
        </>
      }
      maxWidth={320}
    >
      <button
        type="button"
        onClick={onOpen}
        aria-label="Open command palette"
        className={clsx(
          "flex h-7 items-center gap-1.5 rounded-2 border bg-bg-2 px-2 text-[11px] transition-colors",
          trimmed ? "border-accent/50 text-fg-1" : "border-line-2 text-fg-3 hover:bg-bg-3 hover:text-fg-1",
          className,
        )}
      >
        <Icon icon={Search} size={11} />
        {trimmed ? (
          <span className="max-w-[180px] truncate font-mono text-[11px] text-fg-0">{trimmed}</span>
        ) : (
          <span>Search anything…</span>
        )}
        <kbd className="ml-0.5 rounded-1 border border-line-3 bg-bg-3 px-1 py-px font-mono text-[9.5px] text-fg-3">
          ⌘K
        </kbd>
      </button>
    </Tooltip>
  );
}
