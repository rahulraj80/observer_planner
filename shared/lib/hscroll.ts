// Pure geometry for a horizontally scrolling strip (the TabStrip, and the
// SectionRail's chip row below its breakpoint). No DOM access: the hook in
// shared/primitives/useStripScroll.ts measures the elements and feeds the
// numbers in, so the decisions are table-testable in node.

/** Width of the edge fade, in px, and the inset a revealed item keeps from it.
 *  Also the width of the end pad an overflowing strip renders after its last
 *  item, so even the LAST item can scroll fully clear of the fade. */
export const EDGE_FADE_PX = 24;

/** The measured box of a strip and the item to reveal, all in px. */
export type RevealInput = {
  /** The strip's current scrollLeft. */
  scrollLeft: number;
  /** The strip's visible width (clientWidth). */
  clientWidth: number;
  /** The strip's full content width (scrollWidth). */
  scrollWidth: number;
  /** The item's left edge in the strip's content coordinates (offsetLeft). */
  itemLeft: number;
  /** The item's width (offsetWidth). */
  itemWidth: number;
  /** Space to keep between the item and the visible edge (the fade). */
  inset?: number;
};

/**
 * revealScrollLeft returns the scrollLeft that brings the item fully into
 * view (clear of the edge fade), or null when it is already visible and the
 * strip should not move. It scrolls the minimum distance: an item off to the
 * right lands at the right edge, one off to the left lands at the left edge.
 * An item wider than the visible width aligns its left edge. A target within
 * one fade width of either end snaps to that end, so no fade is left over the
 * item. The result is clamped to the scrollable range.
 */
export function revealScrollLeft(r: RevealInput): number | null {
  const inset = Math.max(0, r.inset ?? EDGE_FADE_PX);
  const max = Math.max(0, r.scrollWidth - r.clientWidth);
  if (max === 0) return null;
  const viewStart = r.scrollLeft + inset;
  const viewEnd = r.scrollLeft + r.clientWidth - inset;
  const itemEnd = r.itemLeft + r.itemWidth;
  let target: number;
  if (r.itemWidth > r.clientWidth - 2 * inset) {
    // Too wide to fit between the fades: show its start.
    target = r.itemLeft - inset;
  } else if (r.itemLeft < viewStart) {
    target = r.itemLeft - inset;
    // Within one fade width of the start, go all the way (see below).
    if (target <= inset) target = 0;
  } else if (itemEnd > viewEnd) {
    target = itemEnd - r.clientWidth + inset;
    // Within one fade width of the end, go all the way. The strip only draws
    // a fade at an end it has not reached, so stopping a few px short (the
    // end pad's width never matches the inset exactly once the flex gap is
    // counted) left the end fade drawn over the last tab's trailing label, a
    // count like "0 of 3". At the very end there is no fade, and the end pad
    // already keeps the item clear of the edge.
    if (max - target <= inset) target = max;
  } else {
    return null;
  }
  const clamped = Math.min(max, Math.max(0, Math.round(target)));
  return clamped === Math.round(r.scrollLeft) ? null : clamped;
}

/**
 * contentOverflows reports whether a strip's REAL content (its items, not the
 * end pad) is wider than the strip. The end pad (`StripEndPad` in
 * shared/primitives/useStripScroll.ts) is rendered only while this is true,
 * so it is measured out here: otherwise the pad would keep the strip
 * overflowing by itself. A 1px tolerance absorbs sub-pixel widths.
 */
export function contentOverflows(m: {
  scrollWidth: number;
  clientWidth: number;
  /** Width of the end pad currently rendered (0 when none). */
  padWidth?: number;
}): boolean {
  return m.scrollWidth - Math.max(0, m.padWidth ?? 0) > m.clientWidth + 1;
}

/** What moved the strip's layout, for `stripRevealMode`. */
export type StripLayoutEvent =
  /** The first run (mount, or a cold deep link). */
  | "mount"
  /** The selected item changed (a click, an arrow key, Back/Forward). */
  | "active-change"
  /** The strip went from display:none (width 0) to visible. */
  | "shown"
  /** The strip or its content changed size: a count, dot or label suffix
   *  rendered after the data arrived, a tab appeared, the viewport resized. */
  | "resize";

/**
 * stripRevealMode decides whether (and how) a strip scrolls its active item
 * back into view after a layout event: "instant", "smooth", or null (leave
 * the strip where it is). A resize re-reveals instantly, so a count that
 * renders after the reveal cannot leave the active tab half under the fade,
 * UNLESS the user has scrolled the strip by hand since the active item last
 * changed: their scroll position then wins until they pick an item.
 */
export function stripRevealMode(
  event: StripLayoutEvent,
  s: {
    /** The user scrolled the strip by hand since the active item changed. */
    userScrolled: boolean;
    /** prefers-reduced-motion is on. */
    reducedMotion?: boolean;
    /** A smooth reveal is still animating (the active tab turning bold
     *  resizes it mid-animation): re-target smoothly instead of jumping. */
    smoothInFlight?: boolean;
  },
): "instant" | "smooth" | null {
  switch (event) {
    case "mount":
    case "shown":
      return "instant";
    case "active-change":
      return s.reducedMotion ? "instant" : "smooth";
    case "resize":
      if (s.userScrolled) return null;
      return s.smoothInFlight && !s.reducedMotion ? "smooth" : "instant";
  }
}

/** Which ends of a strip hide content (and so get an edge fade). */
export type OverflowEdges = { start: boolean; end: boolean };

/**
 * overflowEdges reports whether content is hidden past the start and/or the
 * end of the strip. A 1px tolerance absorbs sub-pixel scroll positions.
 */
export function overflowEdges(m: {
  scrollLeft: number;
  clientWidth: number;
  scrollWidth: number;
}): OverflowEdges {
  return {
    start: m.scrollLeft > 1,
    end: m.scrollLeft + m.clientWidth < m.scrollWidth - 1,
  };
}

/**
 * edgeFadeMask is the CSS mask-image that fades the strip's hidden ends, or
 * undefined when nothing overflows. A mask (rather than a coloured overlay)
 * works on any background: a page, a card or a drawer.
 */
export function edgeFadeMask(edges: OverflowEdges, px: number = EDGE_FADE_PX): string | undefined {
  if (!edges.start && !edges.end) return undefined;
  const left = edges.start ? `transparent 0, #000 ${px}px` : "#000 0";
  const right = edges.end ? `#000 calc(100% - ${px}px), transparent 100%` : "#000 100%";
  return `linear-gradient(to right, ${left}, ${right})`;
}
