// Scroll lock shared by the overlay primitives. DOM-only, no React.

/** OVERLAY_OPEN_ATTR is the body attribute every open Modal / SlideOver
 *  holds (a count, so nested overlays nest). Floating chrome that would sit
 *  over an overlay's footer (the node dashboard's "New terminal" dock) hides
 *  itself while it is present, through one CSS rule keyed on it. */
export const OVERLAY_OPEN_ATTR = "data-overlay-open";

type DatasetHolder = { dataset: Record<string, string | undefined> };

/** markOverlay counts one open overlay on `body` (data-overlay-open) and
 *  returns the undo. The undo is idempotent and removes the attribute when
 *  the last overlay closes, so a stray double undo can neither leave the
 *  mark behind nor clear it under an overlay that is still open. */
export function markOverlay(body: DatasetHolder): () => void {
  const read = () => Number.parseInt(body.dataset.overlayOpen ?? "0", 10) || 0;
  body.dataset.overlayOpen = String(read() + 1);
  let done = false;
  return () => {
    if (done) return;
    done = true;
    const n = read() - 1;
    if (n > 0) body.dataset.overlayOpen = String(n);
    else delete body.dataset.overlayOpen;
  };
}

// lockScroll stops the page behind an overlay (SlideOver, Modal) from
// scrolling: `body` plus every ancestor of the panel that is itself a
// vertical scroll container (web2's shell scrolls <main>, not the document). Returns the undo, which
// restores each element's previous inline overflow exactly. It also marks the
// overlay open on body (markOverlay) for floating chrome that must step aside.
export function lockScroll(panel: HTMLElement | null): () => void {
  const targets: HTMLElement[] = [document.body];
  for (let el = panel?.parentElement ?? null; el && el !== document.body; el = el.parentElement) {
    const oy = window.getComputedStyle(el).overflowY;
    if ((oy === "auto" || oy === "scroll") && el.scrollHeight > el.clientHeight) targets.push(el);
  }
  const prev = targets.map((el) => el.style.overflow);
  for (const el of targets) el.style.overflow = "hidden";
  const unmark = markOverlay(document.body);
  return () => {
    targets.forEach((el, i) => {
      el.style.overflow = prev[i];
    });
    unmark();
  };
}
