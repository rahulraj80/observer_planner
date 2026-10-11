// windowsHomes: the Connected tools Windows-home control's reading of GET
// /api/setup/windows-homes (T-421 item 4), the dashboard twin of `observer
// init --windows-claude-home | --windows-codex-home | --windows-cursor-home
// <path>` on a WSL daemon. Pure (no React), so the node test runner covers it.

export type WindowsHomeCandidate = { home: string; owned: boolean };

export type WindowsHomeTarget = {
  target: string;
  subdir: string;
  candidates: WindowsHomeCandidate[];
  /** The home `observer init` auto-picks (exactly one owned candidate). */
  resolved?: string;
  flag: string;
  command: string;
};

export type WindowsHomesResponse = {
  confirm_token?: string;
  wsl: boolean;
  targets: WindowsHomeTarget[];
};

export type WindowsHomeApplyReply = {
  target: string;
  home: string;
  dry_run: boolean;
  lines: string[];
  error?: string;
};

/** windowsHomesVisible: the control shows only on a WSL daemon with targets. */
export function windowsHomesVisible(r: WindowsHomesResponse | null | undefined): boolean {
  return !!r && r.wsl && Array.isArray(r.targets) && r.targets.length > 0;
}

/**
 * defaultHome is the pre-selected home: the one `observer init` would pick on
 * its own, else nothing (the operator chooses; never a guess among several).
 */
export function defaultHome(t: WindowsHomeTarget): string {
  if (t.resolved) return t.resolved;
  return "";
}

/** needsChoice: `observer init` will not auto-pick for this target. */
export function needsChoice(t: WindowsHomeTarget): boolean {
  return !t.resolved;
}

/** cliEquivalent is the exact CLI flag form of the chosen write. */
export function cliEquivalent(t: WindowsHomeTarget, home: string): string {
  return home ? `observer init ${t.flag} ${home}` : t.command;
}

/** previewLines renders an apply reply's result lines, never an empty box. */
export function previewLines(r: WindowsHomeApplyReply | null): string[] {
  if (!r) return [];
  const lines = (r.lines ?? []).filter((l) => l.trim() !== "");
  return lines.length > 0 ? lines : [`${r.target}: nothing to write for this home`];
}
