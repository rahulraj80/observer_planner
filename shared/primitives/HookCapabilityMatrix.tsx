import type { ReactNode } from "react";
import { Pill } from "./Pill";
import { Table } from "./Table";
import { ToolBadge } from "./ToolBadge";
import { Tooltip } from "./Tooltip";
import {
  HOOK_ALT_SEAM,
  HOOK_COVERAGE_STATE,
  HOOK_DECISION_LETTERS,
  hookCellState,
  type HookMatrixCell,
  type HookMatrixRow,
  type HookMatrixView,
} from "../lib/hookMatrix";

// HookCapabilityMatrix - the hook capability matrix (one row per tool, one
// column per capability group), rendered from the generated view
// (shared/lib/hookMatrix.ts). Pure: the caller fetches, filters rows and may
// add a trailing per-row cell (the node's doctor / probe actions, the org's
// failure counts). The same component renders the node Security page and the
// org Control coverage tab, so both read the registry's bindings the same way.

function CellTip({ cell, label }: { cell: HookMatrixCell; label: string }) {
  const st = hookCellState(cell.state);
  return (
    <div className="space-y-1">
      <div className="font-semibold">{label}: {st.label}</div>
      {cell.events.length > 0 && (
        <ul className="space-y-0.5 font-mono text-[10.5px]">
          {cell.events.map((e) => (
            <li key={e}>{e}</li>
          ))}
        </ul>
      )}
      {cell.alt_seam && <div>Other seam: {HOOK_ALT_SEAM[cell.alt_seam] ?? cell.alt_seam}</div>}
    </div>
  );
}

function MatrixCellView({ cell, label }: { cell: HookMatrixCell | undefined; label: string }) {
  if (!cell || cell.state === "none") {
    return <span className="text-fg-4">-</span>;
  }
  const st = hookCellState(cell.state);
  return (
    <Tooltip content={<CellTip cell={cell} label={label} />} maxWidth={360}>
      <span className="inline-flex items-center gap-1" tabIndex={0}>
        <Pill variant={st.tone} case="none">
          {st.mark}
        </Pill>
        {cell.events.length > 1 && <span className="text-[10px] text-fg-3">{cell.events.length}</span>}
      </span>
    </Tooltip>
  );
}

/** HookCapabilityMatrixLegend explains the marks and the decision letters. */
export function HookCapabilityMatrixLegend() {
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-[11px] text-fg-3">
      {Object.entries(HOOK_COVERAGE_STATE)
        .filter(([, v]) => v.mark !== "")
        .map(([k, v]) => (
          <span key={k} className="inline-flex items-center gap-1">
            <Pill variant={v.tone} case="none">
              {v.mark}
            </Pill>
            <span>{v.label}</span>
          </span>
        ))}
      <span>
        Hover a cell for its vendor events. Decisions:{" "}
        {HOOK_DECISION_LETTERS.map(([l, n]) => `${l} ${n}`).join(", ")}.
      </span>
    </div>
  );
}

/** HookCapabilityMatrix renders the matrix rows of `matrix` (or `rows`, a
 *  caller-filtered subset). `trailing` adds one right-hand cell per row
 *  under the `trailingHead` header. */
export function HookCapabilityMatrix({
  matrix,
  rows,
  trailing,
  trailingHead,
  maxHeight,
}: {
  matrix: HookMatrixView;
  rows?: HookMatrixRow[];
  trailing?: (row: HookMatrixRow) => ReactNode;
  trailingHead?: ReactNode;
  maxHeight?: number;
}) {
  const shown = rows ?? matrix.rows;
  return (
    <Table
      minWidth={180 + matrix.groups.length * 92 + (trailing ? 200 : 0)}
      maxHeight={maxHeight}
      stickyHead={maxHeight !== undefined}
      head={
        <tr>
          <th className="py-1.5 pr-3 font-medium">Tool</th>
          {matrix.groups.map((g) => (
            <th key={g.id} className="py-1.5 pr-3 font-medium">
              <Tooltip content={`Capabilities: ${g.caps.join(", ")}`}>
                <span tabIndex={0}>{g.label}</span>
              </Tooltip>
            </th>
          ))}
          {trailing && <th className="py-1.5 text-right font-medium">{trailingHead}</th>}
        </tr>
      }
    >
      {shown.map((row) => {
        const byGroup = new Map(row.cells.map((c) => [c.group, c]));
        return (
          <tr key={row.tool} className="border-b border-line-1/60 last:border-0">
            <td className="py-1.5 pr-3 align-top">
              <div className="flex flex-col items-start gap-0.5">
                <ToolBadge tool={row.tool} />
                {row.mechanism && (
                  <span className="font-mono text-[10px] text-fg-3">
                    {row.mechanism}
                    {!row.auto_wired && " (manual)"}
                  </span>
                )}
              </div>
            </td>
            {matrix.groups.map((g) => (
              <td key={g.id} className="py-1.5 pr-3 align-top">
                <MatrixCellView cell={byGroup.get(g.id)} label={g.label} />
              </td>
            ))}
            {trailing && <td className="py-1.5 text-right align-top">{trailing(row)}</td>}
          </tr>
        );
      })}
    </Table>
  );
}

/** HookCapabilitySummary renders the per-capability counts: how many rows
 *  the vendor surface provides the capability on and how many Observer
 *  wires, registers, or still has to probe. */
export function HookCapabilitySummary({ matrix }: { matrix: HookMatrixView }) {
  return (
    <Table
      minWidth={520}
      size="sm"
      head={
        <tr>
          <th className="py-1.5 pr-3 font-medium">Capability</th>
          <th className="py-1.5 pr-3 text-right font-medium">Vendor provides</th>
          <th className="py-1.5 pr-3 text-right font-medium">Wired</th>
          <th className="py-1.5 pr-3 text-right font-medium">Registered only</th>
          <th className="py-1.5 pr-3 text-right font-medium">Probe</th>
          <th className="py-1.5 text-right font-medium">Gap</th>
        </tr>
      }
    >
      {matrix.summaries.map((s) => (
        <tr key={s.cap} className="border-b border-line-1/60 last:border-0">
          <td className="py-1 pr-3 font-mono text-fg-1">{s.cap}</td>
          <td className="py-1 pr-3 text-right tabular-nums text-fg-2">{s.provides}</td>
          <td className="py-1 pr-3 text-right tabular-nums text-success">{s.wired || "-"}</td>
          <td className="py-1 pr-3 text-right tabular-nums text-info">{s.registered || "-"}</td>
          <td className="py-1 pr-3 text-right tabular-nums text-warn">{s.probe_required || "-"}</td>
          <td className="py-1 text-right tabular-nums text-fg-3">{s.gap || "-"}</td>
        </tr>
      ))}
    </Table>
  );
}
