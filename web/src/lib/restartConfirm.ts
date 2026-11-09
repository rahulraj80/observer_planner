import type { RestartStatus } from "./useDaemonRestart";

// restartConfirm: the copy of the daemon-restart question (useDaemonRestart
// asks it through the shared confirmDialog). Pure, so the wording for each
// probe outcome is unit-tested.
//
// The daemon IS the proxy on its port, so the dialog says what a restart
// does to a coding session routed through it, with the live count from GET
// /api/admin/restart. It does not automate the route OFF/ON dance (lane L5
// B-R2: that needs the route writers to run before the self re-exec and
// again in the new process once its proxy serves, which lives in the start
// path, not here). The dialog says so, gives the practical way to avoid a
// dropped request (restart between turns), and names the source checkout's
// scripts/restart-daemon.sh together with why it runs from a shell.

/** RestartConfirm is what a caller says about its own restart: the dialog's
 *  question and the one line on why (pending config, a new binary). */
export type RestartConfirm = { title: string; lead: string };

/** restartConfirmBody returns the dialog's paragraphs: the caller's lead,
 *  what a restart does, then the proxy line for the probe outcome (null when
 *  the probe was unavailable: older daemon, read-only surface). */
export function restartConfirmBody(lead: string, status: RestartStatus | null): string[] {
  const out = [
    lead,
    "The daemon shuts down gracefully and starts again in place; this page reconnects on its own when the new process answers, typically within a second.",
  ];
  if (status == null) {
    out.push("An active coding session routed through the observer proxy may drop one in-flight request.");
    return out;
  }
  const n = status.proxy_recent_requests ?? 0;
  if (n > 0) {
    out.push(
      `A coding session is routing through this daemon right now (${n} request${n === 1 ? "" : "s"} in the last minute). ` +
        "Restarting drops its in-flight proxy requests.",
    );
    out.push(
      "To avoid that, restart between turns: the new process answers in about a second. " +
        "This restart does not take the proxy route off first. The source checkout's scripts/restart-daemon.sh does " +
        "(route off, restart, route on); it runs from a shell because it stops and relaunches the daemon from outside it.",
    );
  } else {
    out.push("No proxied requests in the last minute, so no coding session should notice.");
  }
  return out;
}
