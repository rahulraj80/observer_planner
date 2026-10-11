// toolProbe: the label of one Settings > Connected tools integration cell
// (GET /api/tools/status ToolProbe), as a pure table so the cell and its
// test read the same rules. Pure: no React, runs under `node --test`.

export type ToolProbeState = {
  registered: boolean;
  partial?: boolean;
  /** No hook surface Observer registers; `detail` says why (hooks only). */
  no_hooks?: boolean;
  /** A hook surface this panel does not dry-run; `detail` says why. */
  unprobed?: boolean;
  detail?: string;
};

export type ProbeVariant = "success" | "warn" | "neutral";

export type ProbeLabel = { label: string; variant: ProbeVariant };

// Ordered: the first row whose `when` holds decides (T-400). `no_hooks` and
// `unprobed` come first because the server never sets them together with a
// registration state.
const PROBE_LABELS: ReadonlyArray<{ when: (p: ToolProbeState) => boolean } & ProbeLabel> = [
  { when: (p) => !!p.no_hooks, label: "no hooks", variant: "neutral" },
  { when: (p) => !!p.unprobed, label: "not probed", variant: "neutral" },
  { when: (p) => p.registered, label: "on", variant: "success" },
  { when: (p) => !!p.partial, label: "partial", variant: "warn" },
  { when: () => true, label: "off", variant: "neutral" },
];

/** probeLabel returns the pill one integration cell renders. */
export function probeLabel(p: ToolProbeState): ProbeLabel {
  const row = PROBE_LABELS.find((r) => r.when(p)) ?? PROBE_LABELS[PROBE_LABELS.length - 1];
  return { label: row.label, variant: row.variant };
}
