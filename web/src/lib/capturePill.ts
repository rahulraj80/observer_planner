// capturePill - the pure model behind the top bar's capture pill
// (components/TopBar.tsx CaptureStatePill), fed by GET /api/setup/claude.
//
// The pill reports ONE thing: whether Claude Code is set up to route through
// this node's proxy. It used to read "Paused" for every status that was not
// ready, so a machine without Claude Code at all (node 1, 2026-10-06:
// status claude_not_installed) looked as if capture had stopped, while the
// watcher, the hooks and every other tool were capturing normally. Each
// status now has its own row: a label that says what it is, and a tooltip
// that says what it does and does not mean. Table-driven (CLAUDE.md #5).
//
// T-803: "No Claude Code" sat in the Overview header of a node whose
// dashboard was full of Claude Code sessions (captured from another OS or
// imported; the daemon only looks for a `claude` binary on its own PATH).
// The pill's subject is this machine's Claude Code proxy route, and with no
// binary here there is no route to report, so the pill is hidden instead of
// contradicting the data next to it.

/** The dot state, one of liveSignals CAPTURE_DOT's keys. */
export type CapturePillState = "active" | "paused" | "absent";

export type CapturePillView = {
  label: string;
  tooltip: string;
  state: CapturePillState;
  /** True when there is nothing for the pill to report: render no pill. */
  hidden?: boolean;
};

const OTHER_TOOLS = "The watcher and the hooks capture every other tool either way.";

const ROWS: Record<string, CapturePillView> = {
  oauth_ready: {
    label: "Active",
    tooltip: "Claude Code routes through this machine's proxy (signed-in account): capturing.",
    state: "active",
  },
  api_key_ready: {
    label: "Active",
    tooltip: "Claude Code routes through this machine's proxy (API key): capturing.",
    state: "active",
  },
  claude_not_installed: {
    label: "No Claude Code here",
    tooltip: `Claude Code was not found on this machine, so there is no Claude Code proxy route to show. ${OTHER_TOOLS}`,
    state: "absent",
    hidden: true,
  },
};

/** capturePillView maps a /api/setup/claude status onto the pill. A status
 *  this build does not know reads as not routed, naming the status. */
export function capturePillView(status: string | null | undefined): CapturePillView {
  if (!status) {
    return { label: "Checking", tooltip: "Reading the Claude Code proxy setup.", state: "absent" };
  }
  const row = ROWS[status];
  if (row) return row;
  return {
    label: "Not routed",
    tooltip: `Claude Code is not routed through this machine's proxy (status: ${status}). ${OTHER_TOOLS}`,
    state: "paused",
  };
}
