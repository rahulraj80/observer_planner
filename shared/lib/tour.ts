// tour: the pure half of the guided tour both dashboards run (T-447). The
// node dashboard's first-run tour (docs/plans/first-run-guided-tour-plan-2026-07-23.md)
// was promoted here so the org dashboard walks its own steps through the SAME
// overlay and provider (shared/components/tour). Each app owns only its step
// list and which steps a viewer may see.
//
// A step either anchors to a real UI element (a `data-tour` selector) or
// renders a centered card (no target). Bodies are plain strings, rendered as
// text nodes only, never dangerouslySetInnerHTML, so the copy can never become
// an injection surface.

export type TourPlacement = "top" | "bottom" | "left" | "right" | "auto";

export type TourStep = {
  // Stable id (used as a React key + for debugging).
  id: string;
  // CSS selector of the anchor element. Omit for a centered card.
  target?: string;
  // Route to navigate to before showing the step (if not already there).
  // A bare pathname: the overlay compares it to location.pathname, so a
  // tabbed page opens on its default tab (or stays on the tab the reader
  // already has open there).
  path?: string;
  // Preferred bubble placement relative to the target. Default "bottom".
  placement?: TourPlacement;
  // Coach-mark heading.
  title: string;
  // Body copy: plain text, rendered as text nodes only.
  body: string;
  // Padding in px around the spotlight cutout. Default 6.
  spotlightPadding?: number;
};

/** Below this width the desktop rail is absent, so the tour never auto-starts
 *  (it can still be started by hand). */
export const TOUR_AUTO_START_MIN_WIDTH = 1024;

/** tourTarget is the selector for an element marked data-tour={id}. */
export function tourTarget(id: string): string {
  return `[data-tour="${id}"]`;
}

/** clampStepIndex keeps a step index inside [0, total - 1] (0 when empty). */
export function clampStepIndex(index: number, total: number): number {
  if (total <= 0 || !Number.isFinite(index)) return 0;
  return Math.max(0, Math.min(total - 1, Math.floor(index)));
}

/** shouldAutoStart: the first-run gate. Never once finished or skipped, never
 *  on a narrow screen, never when the app opted out. */
export function shouldAutoStart(opts: { enabled: boolean; completed: boolean; width: number }): boolean {
  return opts.enabled && !opts.completed && opts.width >= TOUR_AUTO_START_MIN_WIDTH;
}
