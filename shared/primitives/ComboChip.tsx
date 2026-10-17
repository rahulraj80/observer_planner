import {
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import clsx from "clsx";
import { Check, ChevronDown } from "lucide-react";
import { Icon } from "./Icon";
import { Tooltip } from "./Tooltip";
import { STABLE_SCROLL_Y } from "../lib/scroll";

// ComboChip — filter-chip button + anchored popover combobox.
// Mirrors design/shell.jsx:155-166 + design/app.css:259-275: icon +
// label + value + optional swatch + chevron in the resting chip;
// popover lists options with type-to-filter and keyboard nav.
//
// Single-select; replace useState in parent. Empty/clear is modelled
// as a sentinel option in `options` (typically value="all"). Caller
// owns the option list — the chip just renders + filters + selects.
//
// Keyboard: the search input owns focus while the popover is open
// (role="combobox" + aria-activedescendant over the role="listbox" list);
// ArrowUp/ArrowDown move the active row, Enter picks it, Tab leaves, and
// Escape closes ONLY the popover — the key event is stopped here so an
// enclosing Modal/SlideOver (which listens for Escape on `document`) stays
// open. Escape and a pick return focus to the trigger.

export type ComboOption = {
  value: string;
  label: ReactNode;
  // searchable: lower-cased text the type-ahead input matches against.
  // Pages should derive from label + value + side data so models like
  // "anthropic/claude-opus-4-7" match a search for "opus".
  searchable: string;
  // Optional adornment rendered before the label in the popover row.
  // Useful for ToolDot in the Tool filter.
  leading?: ReactNode;
  // Right-side aux text (e.g. action_count).
  rightMeta?: ReactNode;
  // Optional tooltip on the popover row.
  title?: string;
  // Optional secondary line under the label (e.g. a host list or a status
  // explanation). Truncated to one line while the row is idle and shown in
  // FULL (wrapped) while the row is active — hovered or keyboard-focused —
  // so long text stays readable inside the popover without a tooltip
  // overlapping the list. Optional — rows without it render exactly as
  // before.
  detail?: ReactNode;
  // When true, the row renders dimmed/non-interactive: clicking or pressing
  // Enter on it is a no-op (the row stays visible with its `title` reason —
  // the honest-disabled-copy convention, never simply hidden). Mirrors a
  // native `<option disabled>`. Optional — every existing caller that never
  // sets this keeps its current always-selectable behavior.
  disabled?: boolean;
  // Optional group heading rendered once, above the first row in `options`
  // whose groupLabel differs from the row before it (mirrors a native
  // `<optgroup label>`). Rows without a groupLabel render ungrouped. Grouping
  // follows the ORDER options are given in — callers should pre-sort by
  // group.
  groupLabel?: string;
};

export function ComboChip({
  value,
  onChange,
  options,
  icon,
  label,
  className,
  popoverWidth = 320,
  placeholder = "Filter…",
  emptyHint = "No matches.",
  buttonValueRender,
  fullWidth = false,
  id,
  labelledBy,
  disabled = false,
  hideTriggerLabel = false,
  triggerTooltip,
}: {
  value: string;
  onChange: (next: string) => void;
  options: ComboOption[];
  icon?: ReactNode;
  label: string;
  className?: string;
  popoverWidth?: number;
  placeholder?: string;
  emptyHint?: string;
  // When set, overrides the default chip-value rendering. Receives the
  // currently-selected option (or `undefined` when the selected value
  // isn't in the options list). Lets ToolSelect render a ToolDot +
  // pretty label without coupling that detail into the primitive.
  buttonValueRender?: (selected: ComboOption | undefined) => ReactNode;
  // When true, the trigger stretches to fill its container width (space-
  // between label/value and the chevron) instead of the resting inline chip
  // size — for form-field contexts (a modal, a settings panel) where the
  // control should match the width of adjacent inputs. The popover then
  // matches the trigger's width, so it can never be wider than the field
  // (and the dialog/viewport the field sits in). Default false keeps every
  // existing filter-bar caller pixel-identical.
  fullWidth?: boolean;
  // id for the trigger button, so a form `<label htmlFor>` can point at it.
  id?: string;
  // id of an external visible label. When set, the trigger's accessible name
  // is that label PLUS the selected value (aria-labelledby), and the listbox
  // is labelled by it too.
  labelledBy?: string;
  // Disables the trigger (the popover cannot open). Callers state WHY in
  // visible copy next to the control — a disabled button cannot show a
  // hover tooltip.
  disabled?: boolean;
  // Hides the small in-chip `label` prefix — for form fields that already
  // carry a visible `<label>` above the control. `label` still names the
  // listbox for assistive tech.
  hideTriggerLabel?: boolean;
  // Overrides the trigger's hover tooltip. Omitted = the default
  // "<label>: <value>" tooltip; null = no tooltip.
  triggerTooltip?: ReactNode | null;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIdx, setActiveIdx] = useState(0);
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const uid = useId().replace(/:/g, "");
  const listId = `combo-${uid}-list`;
  const valueId = `combo-${uid}-value`;
  const optId = (i: number) => `combo-${uid}-opt-${i}`;

  const selected = options.find((o) => o.value === value);

  // Every whitespace-separated term must appear somewhere in `searchable`
  // (order-free), so "cursor ide" finds a row whose haystack reads
  // "cursor cursor-ide ide". A single-term query behaves exactly as the old
  // plain substring match, and any old match still matches.
  const filtered = useMemo(() => {
    const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
    if (terms.length === 0) return options;
    return options.filter((o) => terms.every((t) => o.searchable.includes(t)));
  }, [options, query]);

  // Reset state when the popover opens/closes so each open starts
  // fresh with the active row at the top (the first ENABLED row, so a
  // leading disabled group header row doesn't eat the first Enter).
  useEffect(() => {
    if (open) {
      setQuery("");
      const firstEnabled = options.findIndex((o) => !o.disabled);
      setActiveIdx(firstEnabled >= 0 ? firstEnabled : 0);
      // Focus the search input on next paint so the autofocus
      // doesn't fight the popover's mount animation.
      requestAnimationFrame(() => inputRef.current?.focus());
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // Keep the popover inside the viewport. It is anchored to the chip's left
  // edge, so on a phone a chip that wrapped to the middle of the filter bar
  // (or a popover wider than the screen) used to push the page sideways.
  // Measured once per open: shift left by the overflow, never past the
  // viewport's left margin; maxWidth below caps a popover wider than the
  // viewport itself. Desktop layouts never overflow, so shiftX stays 0.
  const popoverRef = useRef<HTMLDivElement>(null);
  const [shiftX, setShiftX] = useState(0);
  useLayoutEffect(() => {
    if (!open || fullWidth) {
      setShiftX(0);
      return;
    }
    const el = popoverRef.current;
    if (!el || typeof window === "undefined") return;
    const margin = 8;
    const rect = el.getBoundingClientRect();
    const vw = document.documentElement.clientWidth || window.innerWidth;
    const overflow = rect.right - (vw - margin);
    setShiftX(overflow > 0 ? -Math.max(0, Math.min(overflow, rect.left - margin)) : 0);
  }, [open, fullWidth, popoverWidth]);

  // A disabled trigger never holds an open popover.
  useEffect(() => {
    if (disabled && open) setOpen(false);
  }, [disabled, open]);

  // Click-outside closes.
  useEffect(() => {
    if (!open) return;
    function onDown(e: MouseEvent) {
      if (!rootRef.current) return;
      if (!rootRef.current.contains(e.target as Node)) setOpen(false);
    }
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);

  // Keep the active option in view as the user navigates.
  useEffect(() => {
    if (!open || !listRef.current) return;
    const el = listRef.current.querySelector<HTMLButtonElement>(
      `[data-idx="${activeIdx}"]`,
    );
    el?.scrollIntoView({ block: "nearest" });
  }, [open, activeIdx]);

  // closeAndRefocus closes the popover and hands focus back to the trigger,
  // so keyboard users land where they started instead of on <body>.
  function closeAndRefocus() {
    setOpen(false);
    requestAnimationFrame(() => triggerRef.current?.focus());
  }

  function selectIdx(i: number) {
    const opt = filtered[i];
    // A disabled row (mirrors native `<option disabled>`) can be highlighted
    // via keyboard/hover but never actually chosen — no onChange, popover
    // stays open so the user can pick a different row.
    if (!opt || opt.disabled) return;
    onChange(opt.value);
    closeAndRefocus();
  }

  // nextEnabledIdx walks from `from` in `dir` (+1/-1), skipping disabled
  // rows, and returns the first enabled index found (or `from` unchanged if
  // every remaining row in that direction is disabled).
  function nextEnabledIdx(from: number, dir: 1 | -1): number {
    let i = from;
    while (i + dir >= 0 && i + dir <= filtered.length - 1) {
      i += dir;
      if (!filtered[i]?.disabled) return i;
    }
    return from;
  }

  function onKey(e: React.KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActiveIdx((i) => nextEnabledIdx(Math.min(i, filtered.length - 1), 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActiveIdx((i) => nextEnabledIdx(i, -1));
    } else if (e.key === "Enter") {
      e.preventDefault();
      selectIdx(activeIdx);
    } else if (e.key === "Escape") {
      e.preventDefault();
      // Escape belongs to the popover while it is open: stop it here so an
      // enclosing dialog's document-level Escape handler does not ALSO
      // close the dialog.
      e.stopPropagation();
      closeAndRefocus();
    } else if (e.key === "Tab") {
      // Tab leaves the combobox (rows are not tab stops); close behind it.
      setOpen(false);
    }
  }

  const tooltipContent =
    triggerTooltip !== undefined
      ? triggerTooltip
      : `${label}: ${selected?.searchable ?? value}`;
  const activeOptionId =
    open && filtered[activeIdx] ? optId(activeIdx) : undefined;

  return (
    <div ref={rootRef} className={clsx("relative", fullWidth && "w-full", className)}>
      <Tooltip
        content={tooltipContent}
        disabled={open || disabled || tooltipContent == null}
      >
      <button
        ref={triggerRef}
        id={id}
        type="button"
        disabled={disabled}
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? listId : undefined}
        aria-labelledby={labelledBy ? `${labelledBy} ${valueId}` : undefined}
        className={clsx(
          "flex h-7 items-center gap-1.5 rounded-2 border bg-bg-2 px-2 text-[11px] text-fg-1 transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring",
          fullWidth && "w-full justify-between",
          disabled
            ? "cursor-not-allowed border-line-2 opacity-60"
            : open
              ? "border-accent"
              : "border-line-2 hover:bg-bg-3 hover:text-fg-0",
        )}
      >
        <span className={clsx("flex min-w-0 items-center gap-1.5", fullWidth && "flex-1 truncate")}>
          {icon}
          {!hideTriggerLabel && <span className="text-fg-3">{label}</span>}
          <span id={valueId} className="contents">
            {buttonValueRender ? (
              buttonValueRender(selected)
            ) : (
              <b className={clsx("font-semibold text-fg-0", fullWidth && "min-w-0 truncate")}>
                {selected?.label ?? value}
              </b>
            )}
          </span>
        </span>
        <Icon icon={ChevronDown} size={10} className="ml-0.5 text-fg-3" />
      </button>
      </Tooltip>

      {open && (
        <div
          ref={popoverRef}
          className={clsx(
            "absolute left-0 top-[calc(100%+4px)] z-50 overflow-hidden rounded-3 border border-line-2 bg-bg-1 shadow-drawer",
            fullWidth && "w-full",
          )}
          style={
            fullWidth
              ? undefined
              : {
                  width: popoverWidth,
                  maxWidth: "calc(100vw - 16px)",
                  transform: shiftX ? `translateX(${shiftX}px)` : undefined,
                }
          }
        >
          <div className="border-b border-line-1 bg-bg-2/60 px-2 py-1.5">
            <input
              ref={inputRef}
              type="search"
              role="combobox"
              aria-expanded
              aria-controls={listId}
              aria-autocomplete="list"
              aria-activedescendant={activeOptionId}
              aria-label={`${label} search`}
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setActiveIdx(0);
              }}
              onKeyDown={onKey}
              placeholder={placeholder}
              className="h-7 w-full appearance-none rounded-2 border border-line-2 bg-bg-1 px-2 text-[11px] text-fg-1 placeholder:text-fg-4 focus:border-accent focus:outline-none"
            />
          </div>
          <div
            ref={listRef}
            id={listId}
            role="listbox"
            aria-label={labelledBy ? undefined : label}
            aria-labelledby={labelledBy}
            className={clsx("max-h-[min(280px,50vh)] py-1", STABLE_SCROLL_Y)}
            onKeyDown={onKey}
            tabIndex={-1}
          >
            {filtered.length === 0 ? (
              <p className="px-3 py-3 text-[11px] text-fg-3">{emptyHint}</p>
            ) : (
              filtered.map((o, i) => {
                const sel = o.value === value;
                const active = i === activeIdx;
                // A group header renders once, right before the first row
                // whose groupLabel differs from the previous (filtered) row's
                // — mirrors a native <optgroup label>. Rows without a
                // groupLabel never trigger a header.
                const prevGroup = i > 0 ? filtered[i - 1].groupLabel : undefined;
                const showGroupHeader = !!o.groupLabel && o.groupLabel !== prevGroup;
                const hasDetail = o.detail != null && o.detail !== false;
                return (
                  <div key={o.value} role="presentation">
                    {showGroupHeader && (
                      <div
                        role="presentation"
                        className="mt-1 px-2.5 pb-1 pt-1.5 text-[10px] font-medium uppercase tracking-wide text-fg-4 first:mt-0"
                      >
                        {o.groupLabel}
                      </div>
                    )}
                    <Tooltip content={o.title ?? null} side="right" disabled={!o.title}>
                    <button
                      type="button"
                      id={optId(i)}
                      data-idx={i}
                      role="option"
                      tabIndex={-1}
                      aria-selected={sel}
                      aria-disabled={o.disabled}
                      onMouseEnter={() => setActiveIdx(i)}
                      onClick={() => selectIdx(i)}
                      className={clsx(
                        "flex w-full gap-2 px-2.5 py-1.5 text-left text-[11.5px] transition-colors",
                        hasDetail ? "items-start" : "items-center",
                        o.disabled
                          ? [
                              "cursor-not-allowed text-fg-4",
                              // A detail line carries the disabled row's
                              // reason, so keep it legible; a bare row keeps
                              // the classic dimmed look.
                              hasDetail ? (active ? "bg-bg-2" : "bg-transparent") : "opacity-50",
                            ]
                          : [
                              active ? "bg-bg-3" : "bg-transparent",
                              sel ? "text-accent" : "text-fg-1",
                            ],
                      )}
                    >
                      {hasDetail && o.leading ? (
                        <span className="mt-[3px] flex shrink-0">{o.leading}</span>
                      ) : (
                        o.leading
                      )}
                      {hasDetail ? (
                        <span className="min-w-0 flex-1">
                          <span className="block truncate">{o.label}</span>
                          <span
                            className={clsx(
                              "mt-0.5 block text-[10.5px] leading-snug text-fg-3",
                              active ? "whitespace-normal break-words" : "truncate",
                            )}
                          >
                            {o.detail}
                          </span>
                        </span>
                      ) : (
                        <span className="min-w-0 flex-1 truncate">{o.label}</span>
                      )}
                      {o.rightMeta && (
                        <span className="shrink-0 font-mono text-[10.5px] text-fg-3">
                          {o.rightMeta}
                        </span>
                      )}
                      {sel && !o.disabled && <Icon icon={Check} size={11} className="shrink-0 text-accent" />}
                    </button>
                    </Tooltip>
                  </div>
                );
              })
            )}
          </div>
        </div>
      )}
    </div>
  );
}
