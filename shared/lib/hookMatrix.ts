import type { Tone } from "./tone.ts";

// hookMatrix - the wire shape and presentation tables of the hook
// capability matrix (internal/controlcoverage/hookmatrix_view.go). One JSON
// shape is served by `observer adapters --hooks --json`, the node dashboard's
// GET /api/guard/hook-coverage and the org GET /api/org/guard/coverage
// (`tool_caps`), so the node Security page and the org Control coverage tab
// render the same generated data through <HookCapabilityMatrix>. Nothing in
// here names a tool: every row, column and event comes from the registry's
// hook bindings.

/** One matrix column: a group of hook capabilities. */
export type HookMatrixGroup = { id: string; label: string; caps: string[] };

/** One (tool, column) cell: the best coverage state among the column's
 *  capabilities, the vendor events with their decision letters, and the
 *  first non-hook seam for the column's primary capability. */
export type HookMatrixCell = {
  group: string;
  state: string;
  mark: string;
  events: string[];
  alt_seam?: string;
};

/** One binding: a vendor event bound to one capability. */
export type HookMatrixBinding = {
  cap: string;
  phase?: string;
  native_event: string;
  decisions: string;
  registered: boolean;
  wired: boolean;
  grounding: string;
  source?: string;
  note?: string;
};

/** One tool's row. */
export type HookMatrixRow = {
  tool: string;
  mechanism?: string;
  auto_wired: boolean;
  note?: string;
  cells: HookMatrixCell[];
  caps: Record<string, { state: string; alt_seam?: string }>;
  bindings: HookMatrixBinding[];
};

/** One capability's counts over the rows in the view. */
export type HookMatrixSummary = {
  cap: string;
  provides: number;
  wired: number;
  registered: number;
  probe_required: number;
  gap: number;
};

/** The matrix payload. */
export type HookMatrixView = {
  groups: HookMatrixGroup[];
  rows: HookMatrixRow[];
  summaries: HookMatrixSummary[];
};

/** HOOK_COVERAGE_STATE - tone, short mark and meaning of each coverage
 *  state (controlcoverage.Coverage*). `none` renders as an empty cell. */
export const HOOK_COVERAGE_STATE: Readonly<Record<string, { tone: Tone; mark: string; label: string }>> = {
  wired: { tone: "success", mark: "W", label: "wired: registered and a feature acts on it" },
  registered: { tone: "info", mark: "R", label: "registered: Observer receives and records it" },
  probe_required: { tone: "warn", mark: "probe", label: "documented; the wire shape is unverified" },
  vendor_only: { tone: "neutral", mark: "vendor", label: "the vendor exposes it; Observer does not register it" },
  none: { tone: "neutral", mark: "", label: "no vendor surface" },
};

/** HOOK_DECISION_LETTERS - the decision letters a binding carries. */
export const HOOK_DECISION_LETTERS: ReadonlyArray<readonly [string, string]> = [
  ["D", "deny"],
  ["A", "ask"],
  ["M", "modify input"],
  ["C", "inject context"],
  ["U", "user-visible message"],
  ["O", "observe only"],
];

/** HOOK_ALT_SEAM - the non-hook seam names (controlcoverage.AltSeam*). */
export const HOOK_ALT_SEAM: Readonly<Record<string, string>> = {
  proxy: "proxy",
  native_compile: "native policy compile",
  sandbox: "sandboxed launch",
  watcher: "watcher (after the fact)",
  capture_lane: "capture lane (after the fact)",
};

/** hookCellState resolves a cell's presentation; unknown states read as
 *  neutral with the raw value, never a guess. */
export function hookCellState(state: string): { tone: Tone; mark: string; label: string } {
  return HOOK_COVERAGE_STATE[state] ?? { tone: "neutral", mark: state, label: state };
}

/** hookRowObserverWired reports whether Observer registers anything on the
 *  row (any cell wired, registered or probe). */
export function hookRowObserverWired(row: HookMatrixRow): boolean {
  return row.cells.some((c) => c.state === "wired" || c.state === "registered" || c.state === "probe_required");
}

/** hookRowHasVendorSurface reports whether the vendor exposes any hook. */
export function hookRowHasVendorSurface(row: HookMatrixRow): boolean {
  return row.cells.some((c) => c.state !== "none");
}

/** hookRowGateWired reports whether a gate binding of the row is wired, the
 *  bindings a gate probe can exercise. `gateCaps` is the posture's gate
 *  capability list (tool_gate, prompt_gate, ...). */
export function hookRowGateWired(row: HookMatrixRow, gateCaps: readonly string[]): boolean {
  return row.bindings.some((b) => b.wired && gateCaps.includes(b.cap));
}
