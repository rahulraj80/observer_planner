import clsx from "clsx";
import { useId, useState, type ReactNode } from "react";
import { ChevronRight } from "lucide-react";
import { Icon } from "./Icon";
import { readDisclosureOpen, writeDisclosureOpen, type DisclosureStorage } from "./copyModel";

// InfoDisclosure: the "Learn more" expander (docs/app-design-system.md
// "Progressive disclosure"). The place a card's or a setting's longer
// explanation goes, so the visible copy stays one line: edge cases,
// guarantees, how a merge works, compatibility notes. At most one per card.
//
// A real disclosure button (aria-expanded + aria-controls), keyboard
// operable, collapsed by default. `persistKey` remembers the viewer's choice
// in this browser (wrapped in try/catch: a private window simply forgets);
// it is a per-viewer convenience, never state anything else depends on.
export type InfoDisclosureProps = {
  /** The expanded content. Plain sentences; a short list is fine. */
  children: ReactNode;
  /** The toggle's text (default "Learn more"). */
  summary?: ReactNode;
  defaultOpen?: boolean;
  /** Remember the open state per viewer under this key. */
  persistKey?: string;
  className?: string;
};

function browserStorage(): DisclosureStorage | null {
  try {
    return typeof window !== "undefined" ? window.localStorage : null;
  } catch {
    return null;
  }
}

export function InfoDisclosure({
  children,
  summary = "Learn more",
  defaultOpen = false,
  persistKey,
  className,
}: InfoDisclosureProps) {
  const [open, setOpen] = useState(() =>
    persistKey ? readDisclosureOpen(browserStorage(), persistKey, defaultOpen) : defaultOpen,
  );
  const panelId = useId();
  const toggle = () => {
    const next = !open;
    setOpen(next);
    if (persistKey) writeDisclosureOpen(browserStorage(), persistKey, next);
  };
  return (
    <div className={className}>
      <button
        type="button"
        aria-expanded={open}
        aria-controls={panelId}
        onClick={toggle}
        className="inline-flex items-center gap-1 rounded-1 text-[11.5px] font-medium text-fg-3 hover:text-fg-1 focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]"
      >
        <Icon
          icon={ChevronRight}
          size={12}
          className={clsx("shrink-0 transition-transform duration-150 motion-reduce:transition-none", open && "rotate-90")}
        />
        {summary}
      </button>
      <div
        id={panelId}
        hidden={!open}
        className="mt-1.5 max-w-prose border-l-2 border-line-2 pl-3 text-[12px] leading-relaxed text-fg-2"
      >
        {open && children}
      </div>
    </div>
  );
}
