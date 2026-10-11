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

// The label text after "watcher " for each state.
export const WATCHER_LINE_LABEL: Record<WatcherLineState, string> = {
  active: "active",
  off: "off",
  unknown: "-",
};
