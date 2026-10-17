import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import clsx from "clsx";
import { Search } from "lucide-react";
import { Icon } from "../../primitives/Icon";
import { STABLE_SCROLL_Y } from "../../lib/scroll";
import { paletteClamp, paletteFlatKeys, paletteMove, type PaletteKey } from "../../lib/paletteModel";

// CommandPaletteShell: the Cmd-K / Ctrl-K modal every app shares. It owns
// the CHROME and the keyboard: the portal and backdrop, focus on open, Esc
// (document-wide, so it works even after focus leaves the input), the
// Up / Down / Home / End / Enter cursor over one flat list built from the
// sections, the active row kept in view, sticky section headers and the key
// hints. The SOURCES are injected: each app builds its sections (pages,
// commands, sessions, org entities, search hits), renders each row's body
// and decides what a row does. The query is controlled by the app (the node
// shares it with its global filter, the org keeps a palette-local one).

export type PaletteRow = {
  /** Stable, unique across all sections. */
  key: string;
  /** The row's content (glyph, label, trailing meta). */
  body: ReactNode;
  onActivate: () => void;
  /** Native tooltip (e.g. a full id behind a shortened one). */
  title?: string;
};

export type PaletteSection = { title: string; rows: PaletteRow[] };

export function CommandPaletteShell({
  open,
  onClose,
  query,
  onQueryChange,
  sections,
  placeholder,
  empty,
  error,
  note,
}: {
  open: boolean;
  onClose: () => void;
  query: string;
  onQueryChange: (q: string) => void;
  /** Sections in display order; empty sections are skipped. */
  sections: readonly PaletteSection[];
  placeholder: string;
  /** Shown when no section has a row (loading, no matches, nothing yet). */
  empty: ReactNode;
  /** A load failure, shown above the rows. */
  error?: string | null;
  /** A trailing status line under the rows (e.g. "Searching org data"). */
  note?: ReactNode;
}) {
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  const shown = useMemo(() => sections.filter((s) => s.rows.length > 0), [sections]);
  const keys = useMemo(() => paletteFlatKeys(shown), [shown]);
  const rows = useMemo(() => shown.flatMap((s) => s.rows), [shown]);

  // Focus + select the search on open; the cursor starts at the top.
  useEffect(() => {
    if (!open) return;
    setActive(0);
    const id = requestAnimationFrame(() => {
      inputRef.current?.focus();
      inputRef.current?.select();
    });
    return () => cancelAnimationFrame(id);
  }, [open]);

  // A new query puts the cursor on its best (first) match.
  useEffect(() => {
    setActive(0);
  }, [query]);

  // Keep the cursor inside a list that narrowed.
  useEffect(() => {
    if (active >= keys.length) setActive(paletteClamp(active, keys.length));
  }, [keys.length, active]);

  // Esc closes from anywhere while open.
  useEffect(() => {
    if (!open) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") {
        e.preventDefault();
        onClose();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  // Keep the active row visible as the cursor moves past the fold.
  useEffect(() => {
    if (!open) return;
    const el = listRef.current?.querySelector<HTMLElement>(`[data-palette-idx="${active}"]`);
    el?.scrollIntoView?.({ block: "nearest" });
  }, [open, active]);

  function activate(idx: number) {
    const row = rows[idx];
    if (!row) return;
    row.onActivate();
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === "ArrowDown" || e.key === "ArrowUp" || e.key === "Home" || e.key === "End") {
      // Home / End stay the input's own caret keys while it has text.
      if ((e.key === "Home" || e.key === "End") && e.target === inputRef.current && query) return;
      e.preventDefault();
      setActive((i) => paletteMove(i, e.key as PaletteKey, keys.length));
    } else if (e.key === "Enter") {
      e.preventDefault();
      activate(active);
    }
  }

  if (!open || typeof document === "undefined") return null;

  let idx = -1;
  const body = (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Command palette"
      className="fixed inset-0 z-[90] grid place-items-start justify-center pt-[12vh]"
      onClick={(e) => {
        // Clicking outside the card closes.
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="sb-fade-in fixed inset-0 -z-10 bg-black/55 backdrop-blur-sm" aria-hidden onClick={onClose} />
      <div className="sb-scale-in w-[min(560px,92vw)] overflow-hidden rounded-3 border border-line-2 bg-bg-1 shadow-drawer">
        <div className="flex items-center gap-2 border-b border-line-1 bg-bg-2/70 px-3">
          <Icon icon={Search} size="sm" className="shrink-0 text-fg-3" />
          <input
            ref={inputRef}
            type="search"
            value={query}
            onChange={(e) => onQueryChange(e.target.value)}
            onKeyDown={onKeyDown}
            placeholder={placeholder}
            aria-label="Search"
            className="h-11 min-w-0 flex-1 appearance-none bg-transparent text-[13px] text-fg-0 placeholder:text-fg-4 focus:outline-none"
          />
          <kbd className="rounded-1 border border-line-3 bg-bg-3 px-1.5 py-0.5 font-mono text-[10px] text-fg-3">Esc</kbd>
        </div>
        <div ref={listRef} className={clsx("max-h-[52vh] p-1.5", STABLE_SCROLL_Y)} onKeyDown={onKeyDown} tabIndex={-1}>
          {error && <p className="px-3 py-2 text-[11px] text-danger">{error}</p>}
          {shown.length === 0 ? (
            <div className="px-3 py-6 text-center text-[12px] text-fg-3">{empty}</div>
          ) : (
            shown.map((sec) => (
              <section key={sec.title}>
                <header className="sticky top-0 z-10 bg-bg-1/95 px-3 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-[0.08em] text-fg-3">
                  {sec.title}
                </header>
                {sec.rows.map((row) => {
                  idx += 1;
                  const i = idx;
                  return (
                    <button
                      key={row.key}
                      type="button"
                      data-palette-idx={i}
                      title={row.title}
                      onMouseEnter={() => setActive(i)}
                      onClick={() => activate(i)}
                      className={clsx(
                        "flex w-full items-center gap-2 rounded-2 px-3 py-2 text-left text-[12.5px] transition-colors",
                        i === active ? "bg-accent-soft text-fg-0" : "text-fg-1",
                      )}
                    >
                      {row.body}
                    </button>
                  );
                })}
              </section>
            ))
          )}
          {note && <p className="px-3 py-1.5 text-[10.5px] text-fg-4">{note}</p>}
        </div>
        <footer className="flex items-center justify-between gap-3 border-t border-line-1 bg-bg-2/40 px-3 py-1.5 text-[10px] text-fg-3">
          <span className="flex items-center gap-1.5">
            <Kbd>↑</Kbd>
            <Kbd>↓</Kbd>
            to navigate
          </span>
          <span className="flex items-center gap-1.5">
            <Kbd>Enter</Kbd>
            to open
          </span>
          <span className="flex items-center gap-1.5">
            <Kbd>⌘K</Kbd>
            to toggle
          </span>
        </footer>
      </div>
    </div>
  );

  // Portal to <body> so the palette overlays the whole app shell without
  // interference from any ancestor stacking context.
  return createPortal(body, document.body);
}

function Kbd({ children }: { children: ReactNode }) {
  return (
    <kbd className="rounded-1 border border-line-3 bg-bg-3 px-1 py-px font-mono text-[9.5px] text-fg-2">{children}</kbd>
  );
}
