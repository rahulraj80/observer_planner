import { useState } from "react";
import clsx from "clsx";
import { ChevronRight, SlidersHorizontal } from "lucide-react";
import { Icon, Tooltip } from "@/components/primitives";
import type { NavItem } from "@/lib/nav";

// The sidebar's Advanced heading (lib/navLayout.ts): one row that folds and
// unfolds the pages most people open rarely, and, when unfolded, a small
// control to keep any of them in the main list instead. The rows themselves
// are the Sidebar's own; this file renders only the heading.

export function NavAdvancedToggle({
  open,
  onToggle,
  folded,
  choices,
  onPin,
  collapsed = false,
  reveal,
}: {
  open: boolean;
  onToggle: () => void;
  /** How many pages the fold hides. */
  folded: number;
  /** Every Advanced page, and whether it is kept in the main list. */
  choices: { item: NavItem; pinned: boolean }[];
  onPin: (id: string, pinned: boolean) => void;
  /** The desktop icon rail: the heading shrinks to its chevron. */
  collapsed?: boolean;
  reveal?: string;
}) {
  const [choosing, setChoosing] = useState(false);
  const what = `${folded} more ${folded === 1 ? "page" : "pages"}`;
  return (
    <div className="mb-2 border-t border-line-1 pt-3">
      <div className="flex items-center gap-1">
        <Tooltip content={collapsed ? `Advanced: ${what}` : null} side="right">
          <button
            type="button"
            onClick={onToggle}
            aria-expanded={open}
            data-tour="nav-advanced"
            className={clsx(
              "flex min-w-0 flex-1 items-center gap-1.5 rounded-2 px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-fg-3 transition-colors hover:bg-bg-2 hover:text-fg-1",
              collapsed && "lg:justify-center",
            )}
          >
            <Icon
              icon={ChevronRight}
              size={12}
              className={clsx("shrink-0 transition-transform duration-base ease-out", open && "rotate-90")}
            />
            <span className={clsx("flex-1 truncate text-left", collapsed && "lg:hidden", reveal)}>
              Advanced
            </span>
            {!open && (
              <span
                title={what}
                aria-label={what}
                className={clsx(
                  "shrink-0 font-mono text-[10px] font-normal tabular-nums text-fg-4",
                  collapsed && "lg:hidden",
                  reveal,
                )}
              >
                {folded}
              </span>
            )}
          </button>
        </Tooltip>
        {open && (
          <Tooltip content="Choose which pages stay in the main list" side="right">
            <button
              type="button"
              onClick={() => setChoosing((v) => !v)}
              aria-expanded={choosing}
              aria-label="Choose which pages stay in the main list"
              className={clsx(
                "grid h-6 w-6 shrink-0 place-items-center rounded-2 transition-colors hover:bg-bg-2 hover:text-fg-1",
                choosing ? "bg-bg-3 text-fg-0" : "text-fg-3",
                collapsed && "lg:hidden",
              )}
            >
              <Icon icon={SlidersHorizontal} size={12} />
            </button>
          </Tooltip>
        )}
      </div>
      {open && choosing && (
        <fieldset
          className={clsx(
            "mt-2 rounded-2 border border-line-1 bg-bg-2 px-2 py-2",
            collapsed && "lg:hidden",
          )}
        >
          <legend className="px-1 text-[10.5px] text-fg-3">Keep in the main list</legend>
          <div className="max-h-56 overflow-y-auto">
            {choices.map(({ item, pinned }) => (
              <label
                key={item.id}
                className="flex cursor-pointer items-center gap-2 rounded-2 px-1 py-1 text-[12px] text-fg-2 hover:bg-bg-3 hover:text-fg-1"
              >
                <input
                  type="checkbox"
                  checked={pinned}
                  onChange={(e) => onPin(item.id, e.target.checked)}
                  className="h-3 w-3 shrink-0 accent-accent"
                />
                <span className="truncate">{item.label}</span>
              </label>
            ))}
          </div>
        </fieldset>
      )}
    </div>
  );
}
