// launchStatus.ts holds the terminal status vocabulary and its two pure
// helpers, split out of components/LaunchTerminal.tsx (lane MP-REL2, T-687)
// so a surface that only needs to NAME a status (the dock in main.tsx, the
// workspace grid) does not import the terminal module itself. That import is
// what kept the xterm chunks in the minimal distro's bundle: LaunchTerminal is
// now reached only through LAUNCH_TERMINAL in lib/components.gen.ts, which is
// null in a distro that does not compile the terminals component.
// LaunchTerminal re-exports everything here, so existing importers compile
// unchanged.

export type Status =
  | "connecting"
  | "open"
  | "reconnecting"
  | "exited"
  | "error";

// PolicyStopInfo is the additive explanation the daemon attaches to a
// terminal's exit event when an org-managed node's OWN node-intervention
// loop stopped this PTY's child (server: internal/intelligence/dashboard
// policystop.go, over the guard_events process_control audit trail). It
// rides the {"t":"exit",…} control frame and GET
// /api/terminal/<handle>/status's `policy_stop` field verbatim, undefined
// means "nothing to show", never a guess.
export type PolicyStopInfo = {
  rule_id: string;
  decision: string;
  reason: string;
  at: string;
};

// policyStopMessage renders the honest, decision-aware sentence a UI surface
// should show, mirrors the server's PolicyStop.DisplayMessage so the
// wording matches regardless of which surface (terminal modal, dock pill,
// status API consumer) renders it. "terminated"/"killed" mean the daemon
// actually stopped the process; any other decision (e.g.
// "policy_unavailable") means the policy check itself failed, so the copy
// must not claim the process was stopped.
export function policyStopMessage(info: PolicyStopInfo): string {
  const stopped = info.decision === "terminated" || info.decision === "killed";
  if (stopped) {
    let msg = "Stopped by organization policy";
    if (info.rule_id) msg += ` (${info.rule_id})`;
    if (info.reason) msg += `: ${info.reason}`;
    return msg;
  }
  let msg = "Organization policy check failed; process was not stopped by the daemon";
  if (info.reason) msg += `: ${info.reason}`;
  return msg;
}

// isLiveStatus is the single predicate for "is this token still live?", i.e.
// its project can still be browsed and it can still be docked/paste-targeted. A
// policy-rejected/errored socket ("error") is just as dead as an "exited" one
// (P2-3): both must gate the project panel off, close an open panel, and revoke
// the paste writer. Undefined/"connecting" count as live (optimistic, matching
// the pre-existing disabled-gate semantics).
//
// "reconnecting" is LIVE by construction (2026-07-25, mobile terminal-continuity
// arc): the PTY is a server-side object that outlives its viewers: the manager
// removes a viewer WITHOUT killing the process, so a dropped websocket says
// nothing at all about the session. Treating a transport drop as death is what
// made a phone returning from its mail app find every terminal tombstoned.
export function isLiveStatus(s: Status | undefined): boolean {
  return s !== "exited" && s !== "error";
}
