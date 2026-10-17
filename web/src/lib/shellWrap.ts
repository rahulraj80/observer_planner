// shellWrap.ts - pure helpers for the Settings -> Terminal "Command wrapping"
// card (backlog item 7). No imports besides `import type`, so it runs under
// plain node:test.

import type { ShellWrapHonesty, ShellWrapStatus, ShellWrapTarget, ShellWrapTool, ShellWrapUnavailable } from "./types.ts";

// The card's target switch (T-419). "native" is the daemon's own shells (the
// API's empty target id); "windows" is the Windows shells of the machine whose
// WSL distro runs the daemon.
export type ShellWrapTargetChoice = "native" | "windows";

export function targetApiId(c: ShellWrapTargetChoice): string {
  return c === "windows" ? "windows" : "";
}

// statusParams is the GET /api/shell-wrap/status query for a choice.
export function statusParams(c: ShellWrapTargetChoice): Record<string, string> | undefined {
  return c === "windows" ? { target: "windows" } : undefined;
}

export type TargetOption = {
  value: ShellWrapTargetChoice;
  label: string;
  disabled?: boolean;
  disabledReason?: string;
};

// targetOptions renders the switch only when there is a second target to
// switch to; an unavailable one stays visible, disabled, with its reason.
export function targetOptions(targets: ShellWrapTarget[] | null | undefined): TargetOption[] {
  const list = targets ?? [];
  if (list.length < 2) return [];
  return list.map((t) => ({
    value: t.id === "windows" ? "windows" : "native",
    label: t.label,
    disabled: !t.available,
    disabledReason: t.available ? undefined : (t.reason ?? "not available here"),
  }));
}

// sideLabel is the small tag a bridge row carries (null on a native target).
export function sideLabel(st: ShellWrapStatus, t: ShellWrapTool): string | null {
  if (!st.bridge) return null;
  return t.side === "wsl" ? "runs in WSL" : "Windows program";
}

export type PillTone = "success" | "warn" | "neutral" | "info";

// Honesty -> Pill tone. The TEXT always comes from the server
// (honesty_text), so no surface re-words what wrapping buys.
export const HONESTY_TONE: Record<ShellWrapHonesty, PillTone> = {
  routed: "success",
  proof_owed: "warn",
  launch_only: "neutral",
  conditional: "info",
};

// selectionKey identifies a (tools, shells) selection independent of order,
// so Apply is only offered for the exact selection that was previewed.
export function selectionKey(tools: string[], shells: string[]): string {
  return [...tools].sort().join(",") + "|" + [...shells].sort().join(",");
}

// stateLabel is the card's badge: an ordered table, first match wins.
const STATE_ROWS: { match: (s: ShellWrapStatus) => boolean; text: string; tone: PillTone }[] = [
  { match: (s) => s.enabled && s.in_sync, text: "on", tone: "success" },
  { match: (s) => s.enabled, text: "on - files differ, re-apply", tone: "warn" },
  { match: (s) => s.active, text: "off - leftovers installed", tone: "warn" },
  { match: () => true, text: "off", tone: "neutral" },
];

export function stateLabel(s: ShellWrapStatus): { text: string; tone: PillTone } {
  const row = STATE_ROWS.find((r) => r.match(s))!;
  return { text: row.text, tone: row.tone };
}

// unavailableRows is the card's "Not wrappable here" list: every advertised
// row the server says has no command to wrap on this target, command-line
// tools first, then IDEs and desktop apps, each by label. The reason and the
// hint are the server's text, never re-worded here.
export function unavailableRows(st: Pick<ShellWrapStatus, "unavailable"> | null | undefined): ShellWrapUnavailable[] {
  const rank = (k: string) => (k === "terminal" ? 0 : 1);
  return [...(st?.unavailable ?? [])].sort(
    (a, b) => rank(a.kind) - rank(b.kind) || a.label.localeCompare(b.label) || a.id.localeCompare(b.id),
  );
}
