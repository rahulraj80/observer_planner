import type { WatcherHealth } from "@/lib/types";

// The sidebar footer's "watcher" line state (T-585). Keyed to the watcher
// health payload's own watcher_running flag, never to the Claude Code
// proxy-route setup status: the file watcher runs whatever any AI tool's
// route is, so "watcher -" next to an unrouted Claude Code was wrong.
//   - "active": the daemon reports it runs the watcher.
//   - "off": the payload loaded and the daemon reports no watcher (a
//     standalone dashboard).
//   - "unknown": no payload yet, or an older daemon that does not report it.
export type WatcherLineState = "active" | "off" | "unknown";

export function watcherLineState(w: WatcherHealth | null | undefined): WatcherLineState {
  if (w == null || typeof w.watcher_running !== "boolean") return "unknown";
  return w.watcher_running ? "active" : "off";
}

// The footer line for each state. T-803: "watcher off" was jargon; the
// line now says what it means for the user (is new activity being
// recorded?) and the tooltip says why.
export const WATCHER_LINE_LABEL: Record<WatcherLineState, string> = {
  active: "Capturing",
  off: "Not capturing",
  unknown: "Checking capture",
};

// The tooltip on that line.
export const WATCHER_LINE_TOOLTIP: Record<WatcherLineState, string> = {
  active: "Observer is watching your AI tools' session files and recording new activity as it happens.",
  off: "This dashboard runs without the file watcher, so it shows what is already recorded but records nothing new. Start Observer with observer start to capture again.",
  unknown: "Reading the watcher status.",
};
