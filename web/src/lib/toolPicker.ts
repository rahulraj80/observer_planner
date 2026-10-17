// toolPicker is the pure option model behind the New terminal dialog's Tool
// picker (NewTerminalDialog.tsx). It turns the three carriers GET
// /api/terminal/sessions reports — launchable_tools + launchable_tool_info
// (terminal AI tools), gui_launchables (IDE / desktop apps) and shell_enabled
// (the separate [terminal.launch].allow_shell opt-in) — into ONE ordered,
// grouped list of picker items.
//
// Why a model instead of option labels: the picker used to be a native
// <select> whose option TEXT carried every annotation ("IntelliJ IDEA [IDE] ·
// not allow-listed · hosts: junie, claude-code, ..."), which the browser-owned
// popup rendered wider than the dialog and the window. Here each annotation is
// a separate field (name, kind badge, status badge, secondary detail line) the
// dialog renders through the shared ComboChip.
//
// Rules this module owns:
//   - Grouping and order come from the GROUPS table (CLI agents, IDEs, Desktop
//     apps, Shell), and within a group launchable rows sort before refused
//     ones, otherwise keeping the server's order (a stable sort).
//   - Statuses come from the server's capability flags (allowed / watched),
//     never from a tool name (CLAUDE.md #3). An absent annotation (an older
//     daemon without launchable_tool_info) is UNKNOWN: no status badge, never
//     "not allowed".
//   - Advertised products the dashboard cannot start (browser chats, editor
//     extensions, desktop apps with no verified launch) are LISTED in their
//     own last group, disabled, with the server's reason as the detail line
//     (T-494, D-160): an honest note instead of a silent omission.
//   - Every AI-tool and GUI row stays SELECTABLE even when the launch
//     allow-list refuses it: installing is not launching (audit DI-21,
//     TestTerminalInstallIsNotGatedByAllowedTools). Only the Shell row and the
//     not-launchable rows can be disabled: when allow_shell is off there is nothing to preflight or
//     install behind it.
//
// No imports besides `import type`, so it runs under the node test runner.

/** One element of launchable_tool_info (mirrors toolInstall.LaunchableToolInfo). */
export type PickerToolInfo = { tool: string; allowed: boolean; watched: boolean };

/** The fields of a gui_launchables row the picker reads (types.GUILaunchable). */
export type PickerGUIRow = {
  id: string;
  label: string;
  adapter: string;
  surface: "ide" | "desktop";
  allowed: boolean;
  watched: boolean;
  hosts: string[];
};

/** The fields of an unlaunchable_tools row the picker reads (types.UnlaunchableTool). */
export type PickerUnlaunchableRow = { tool: string; kind: string; host?: string; note: string };

export type PickerKind = "cli" | "ide" | "desktop" | "shell" | "unlaunchable";

/** Value prefix of an unlaunchable row: never a launch id, never selectable. */
export const UNLAUNCHABLE_PREFIX = "unlaunchable:";

export type PickerStatus = "not_allowed" | "not_captured" | "shell_off";

export type ToolPickerItem = {
  /** The id sent on the launch POST (a tool id, a GUI id, or "shell"). */
  value: string;
  /** Primary line: the human name. */
  name: string;
  /**
   * Tool-vocabulary key the row's colour dot is drawn from: the capturing
   * adapter for a GUI app (so Claude Desktop wears its adapter's colour), else
   * the id itself. An unknown key falls back to the neutral swatch.
   */
  dotKey: string;
  kind: PickerKind;
  /** Small badge text: CLI / IDE / Desktop / Shell. */
  kindLabel: string;
  /** Group heading the row sits under. */
  group: string;
  /** Status badge, or null when there is nothing to flag. */
  status: PickerStatus | null;
  statusLabel: string | null;
  /** Secondary line (a host list, or why the row is off), or null. */
  detail: string | null;
  /** Only the Shell row can be disabled; see the module comment. */
  disabled: boolean;
  /** Lower-cased type-ahead haystack. */
  searchable: string;
};

export type ToolPickerInput = {
  tools: string[];
  toolInfo: PickerToolInfo[];
  gui: PickerGUIRow[];
  shellEnabled: boolean;
  /** The Shell pseudo-tool id (termsvc.ShellTool). */
  shellTool: string;
  /** Display name for a tool id (the shared tool vocabulary, injected). */
  labelFor: (toolId: string) => string;
  /** Products the picker lists but cannot start (absent on an older daemon). */
  unlaunchable?: PickerUnlaunchableRow[];
};

// GROUPS is the one ordering + vocabulary table: a row per kind, walked
// top-down. Adding a kind is a row here, not a branch in the dialog.
export const GROUPS: ReadonlyArray<{ kind: PickerKind; group: string; badge: string }> = [
  { kind: "cli", group: "CLI agents", badge: "CLI" },
  { kind: "ide", group: "IDEs", badge: "IDE" },
  { kind: "desktop", group: "Desktop apps", badge: "Desktop" },
  { kind: "shell", group: "Shell", badge: "Shell" },
  { kind: "unlaunchable", group: "Not launchable here", badge: "Info" },
];

// STATUS_LABELS is the plain-words status vocabulary. "Not allowed to launch"
// replaces the old "not allow-listed" jargon; the inline note under the
// picker says where the switch lives.
export const STATUS_LABELS: Record<PickerStatus, string> = {
  not_allowed: "Not allowed to launch",
  not_captured: "Not captured",
  shell_off: "Turned off",
};

const SHELL_ON_DETAIL = "A plain shell, no AI tool.";
const SHELL_OFF_DETAIL = "Turn on allow_shell in the launch policy to use it.";

function groupOf(kind: PickerKind) {
  return GROUPS.find((g) => g.kind === kind) ?? GROUPS[0];
}

function item(
  kind: PickerKind,
  value: string,
  name: string,
  status: PickerStatus | null,
  detail: string | null,
  disabled: boolean,
  dotKey: string = value,
): ToolPickerItem {
  const g = groupOf(kind);
  const statusLabel = status ? STATUS_LABELS[status] : null;
  return {
    value,
    name,
    dotKey,
    kind,
    kindLabel: g.badge,
    group: g.group,
    status,
    statusLabel,
    detail,
    disabled,
    searchable: [name, value, g.badge, g.group, statusLabel ?? ""].join(" ").toLowerCase(),
  };
}

// hostsDetail renders a host row's agent list ("Agents: Junie, Claude Code,
// ..."). Only an editor host (adapter === "", no capture of its own) carries
// one — the same rule the old option suffix followed.
function hostsDetail(row: PickerGUIRow, labelFor: (id: string) => string): string | null {
  if (row.adapter !== "" || row.hosts.length === 0) return null;
  return `Agents: ${row.hosts.map(labelFor).join(", ")}`;
}

// rank sorts launchable rows before refused ones; everything else keeps its
// incoming order (Array.prototype.sort is stable).
function rank(i: ToolPickerItem): number {
  return i.status === "not_allowed" || i.disabled ? 1 : 0;
}

/** buildToolPickerItems returns the ordered, grouped picker rows. */
export function buildToolPickerItems(input: ToolPickerInput): ToolPickerItem[] {
  const info = new Map(input.toolInfo.map((i) => [i.tool, i]));
  const rows: ToolPickerItem[] = [];

  for (const t of input.tools) {
    const ann = info.get(t);
    const status: PickerStatus | null = !ann
      ? null
      : !ann.allowed
        ? "not_allowed"
        : !ann.watched
          ? "not_captured"
          : null;
    rows.push(item("cli", t, input.labelFor(t), status, null, false));
  }

  for (const r of input.gui) {
    const isHost = r.adapter === "";
    const status: PickerStatus | null = !r.allowed
      ? "not_allowed"
      : !isHost && !r.watched
        ? "not_captured"
        : null;
    rows.push(
      item(
        r.surface === "ide" ? "ide" : "desktop",
        r.id,
        r.label,
        status,
        hostsDetail(r, input.labelFor),
        false,
        r.adapter || r.id,
      ),
    );
  }

  rows.push(
    item(
      "shell",
      input.shellTool,
      "Shell",
      input.shellEnabled ? null : "shell_off",
      input.shellEnabled ? SHELL_ON_DETAIL : SHELL_OFF_DETAIL,
      !input.shellEnabled,
    ),
  );

  for (const u of input.unlaunchable ?? []) {
    rows.push(item("unlaunchable", UNLAUNCHABLE_PREFIX + u.tool, input.labelFor(u.tool), null, u.note, true, u.tool));
  }

  const order = new Map(GROUPS.map((g, i) => [g.kind, i]));
  return rows
    .map((r, i) => ({ r, i }))
    .sort(
      (a, b) =>
        (order.get(a.r.kind) ?? 0) - (order.get(b.r.kind) ?? 0) ||
        rank(a.r) - rank(b.r) ||
        a.i - b.i,
    )
    .map(({ r }) => r);
}

// preferredDefaultTool picks the dialog's initial tool: the first terminal
// tool the launch allow-list accepts (so Start is usable out of the box when
// anything is allowed), else the first listed tool, else "". An absent
// annotation counts as allowed (unknown is never "not allowed").
export function preferredDefaultTool(tools: string[], toolInfo: PickerToolInfo[]): string {
  const info = new Map(toolInfo.map((i) => [i.tool, i]));
  return tools.find((t) => info.get(t)?.allowed !== false) ?? tools[0] ?? "";
}
