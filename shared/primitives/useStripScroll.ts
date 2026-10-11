import {
  createElement,
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type ReactElement,
  type RefObject,
} from "react";
import {
  EDGE_FADE_PX,
  contentOverflows,
  edgeFadeMask,
  overflowEdges,
  revealScrollLeft,
  stripRevealMode,
  type OverflowEdges,
  type StripLayoutEvent,
} from "../lib/hscroll";
import { prefersReducedMotion } from "../lib/motion";

/** How long a smooth reveal is treated as still animating, in ms. */
const SMOOTH_WINDOW_MS = 600;

/**
 * useStripScroll binds a horizontally scrolling strip to its active item:
 *  - it scrolls the active item into view on mount (instantly, so a cold deep
 *    link to the 6th tab does not open with it off-screen) and whenever
 *    `activeKey` changes (smoothly, unless reduced motion is on). It moves
 *    ONLY the strip's own scrollLeft: never the page (scrollIntoView would
 *    also scroll the document vertically);
 *  - it re-reveals the active item whenever the strip or its content changes
 *    size (a count, dot or label suffix rendering after its data arrives, a
 *    tab appearing, the viewport resizing), so a late count cannot push the
 *    active tab back under the fade. A user who scrolled the strip by hand
 *    keeps their position until they pick an item (`stripRevealMode`);
 *  - it tracks which ends overflow and returns the matching mask style for
 *    the edge fade, plus `endPad`: render `<StripEndPad />` as the strip's
 *    LAST child while it is true, so the last item can scroll fully clear of
 *    the end fade.
 *
 * The strip must be the item's offsetParent (give it `position: relative`)
 * so the item's offsetLeft is in the strip's content coordinates.
 * `findActive` returns the active item element (or null for none).
 */
export function useStripScroll(
  stripRef: RefObject<HTMLElement>,
  activeKey: string | null,
  findActive: (strip: HTMLElement) => HTMLElement | null,
): { edges: OverflowEdges; maskStyle: CSSProperties | undefined; endPad: boolean } {
  const [edges, setEdges] = useState<OverflowEdges>({ start: false, end: false });
  const [endPad, setEndPad] = useState(false);
  const first = useRef(true);
  // The user scrolled the strip by hand since the active item last changed.
  const userScrolled = useRef(false);
  // performance.now() until which the last smooth reveal is still animating.
  const smoothUntil = useRef(0);
  // Latest-value refs, so `reveal` keeps one identity across renders.
  const findRef = useRef(findActive);
  findRef.current = findActive;
  const activeKeyRef = useRef(activeKey);
  activeKeyRef.current = activeKey;

  const measure = useCallback(() => {
    const el = stripRef.current;
    if (!el) return;
    const next = overflowEdges(el);
    setEdges((prev) => (prev.start === next.start && prev.end === next.end ? prev : next));
    const pad = el.querySelector<HTMLElement>(":scope > [data-strip-end-pad]");
    const over = contentOverflows({
      scrollWidth: el.scrollWidth,
      clientWidth: el.clientWidth,
      padWidth: pad?.offsetWidth ?? 0,
    });
    setEndPad((prev) => (prev === over ? prev : over));
  }, [stripRef]);

  // Scroll the active item into view after a layout event (strip-local,
  // never the page). Whether and how is the pure stripRevealMode table.
  const reveal = useCallback(
    (event: StripLayoutEvent) => {
      const el = stripRef.current;
      if (!el || activeKeyRef.current == null || el.clientWidth === 0) return;
      const mode = stripRevealMode(event, {
        userScrolled: userScrolled.current,
        reducedMotion: prefersReducedMotion(),
        smoothInFlight: now() < smoothUntil.current,
      });
      if (mode == null) return;
      const item = findRef.current(el);
      if (!item) return;
      const left = revealScrollLeft({
        scrollLeft: el.scrollLeft,
        clientWidth: el.clientWidth,
        scrollWidth: el.scrollWidth,
        itemLeft: item.offsetLeft,
        itemWidth: item.offsetWidth,
        inset: EDGE_FADE_PX,
      });
      if (left == null) return;
      if (mode === "smooth") smoothUntil.current = now() + SMOOTH_WINDOW_MS;
      const behavior = mode === "smooth" ? "smooth" : "auto";
      if (typeof el.scrollTo === "function") el.scrollTo({ left, behavior });
      else el.scrollLeft = left;
    },
    [stripRef],
  );

  useLayoutEffect(() => {
    // Instant on the first run (a cold deep link), smooth on later changes.
    // A new active item also ends any "the user scrolled by hand" hold.
    userScrolled.current = false;
    reveal(first.current ? "mount" : "active-change");
    first.current = false;
    measure();
  }, [activeKey, reveal, measure]);

  // The end pad appearing (or going) changes the scroll range: reveal again
  // so the last item clears the fade.
  useLayoutEffect(() => {
    reveal("resize");
    measure();
  }, [endPad, reveal, measure]);

  useEffect(() => {
    const el = stripRef.current;
    if (!el) return;
    let lastWidth = el.clientWidth;
    const onResize = () => {
      // A strip that was display:none (the SectionRail chip row above its
      // breakpoint) has width 0; when it appears, reveal the active item.
      const w = el.clientWidth;
      const event: StripLayoutEvent = lastWidth === 0 && w > 0 ? "shown" : "resize";
      lastWidth = w;
      reveal(event);
      measure();
    };
    // Hand scrolling (wheel, touch, dragging the scrollbar) holds the
    // position against later resize-driven reveals.
    const onUserScroll = () => {
      userScrolled.current = true;
    };
    const onPointerDown = (e: PointerEvent) => {
      if (e.target === el) userScrolled.current = true;
    };
    el.addEventListener("scroll", measure, { passive: true });
    el.addEventListener("wheel", onUserScroll, { passive: true });
    el.addEventListener("touchmove", onUserScroll, { passive: true });
    el.addEventListener("pointerdown", onPointerDown);
    let ro: ResizeObserver | null = null;
    let mo: MutationObserver | null = null;
    if (typeof ResizeObserver !== "undefined") {
      const observer = new ResizeObserver(onResize);
      ro = observer;
      observer.observe(el);
      // The content can grow without the strip resizing (a count arriving):
      // observe every item, including ones added later (a tab appearing).
      for (const child of Array.from(el.children)) observer.observe(child);
      if (typeof MutationObserver !== "undefined") {
        mo = new MutationObserver((records) => {
          for (const r of records) {
            for (const n of Array.from(r.addedNodes)) {
              if (n instanceof Element) observer.observe(n);
            }
          }
          onResize();
        });
        mo.observe(el, { childList: true });
      }
    } else {
      window.addEventListener("resize", onResize);
    }
    return () => {
      el.removeEventListener("scroll", measure);
      el.removeEventListener("wheel", onUserScroll);
      el.removeEventListener("touchmove", onUserScroll);
      el.removeEventListener("pointerdown", onPointerDown);
      mo?.disconnect();
      if (ro) ro.disconnect();
      else window.removeEventListener("resize", onResize);
    };
  }, [stripRef, measure, reveal]);

  const mask = edgeFadeMask(edges);
  const maskStyle = mask ? { maskImage: mask, WebkitMaskImage: mask } : undefined;
  return { edges, maskStyle, endPad };
}

/**
 * StripEndPad is the empty trailing spacer an overflowing strip renders as
 * its LAST child (while `useStripScroll(...).endPad` is true): it extends the
 * scroll range by the fade width, so the last item can scroll fully past the
 * end fade instead of ending under it. Hidden from assistive tech; the hook
 * measures it out of the overflow decision so it never keeps itself alive.
 */
export function StripEndPad(): ReactElement {
  return createElement("span", {
    "aria-hidden": true,
    "data-strip-end-pad": "",
    className: "pointer-events-none shrink-0",
    style: { width: EDGE_FADE_PX, minWidth: EDGE_FADE_PX },
  });
}

function now(): number {
  return typeof performance !== "undefined" ? performance.now() : Date.now();
}
