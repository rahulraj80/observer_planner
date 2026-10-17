import { useMemo } from "react";
import {
  PageHeader,
  Pill,
  StatCard,
} from "@/components/primitives";
import { useFilters, windowParams, windowLabel } from "@/lib/filters";
import { useApi } from "@/lib/useApi";
import { fmtCompact, fmtInt, fmtPct, fmtUSD } from "@/lib/format";
import { navIcon } from "@/lib/nav";

interface EvidenceMatrixCell {
  kind: string;
  model: string;
  provider: string;
  turns: number;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_creation_tokens: number;
  cost_usd: number;
  error_rate: number;
  avg_ttft_ms: number;
  avg_total_ms: number;
}

interface EvidenceMatrixResponse {
  days: number;
  since: string;
  until: string;
  cells: EvidenceMatrixCell[];
}

const KIND_LABELS: Record<string, string> = {
  plan: "Plan",
  read_only: "Read",
  edit: "Edit",
  test_run: "Test",
  housekeeping: "Housekeeping",
  subagent: "Subagent",
  long_context: "Long-ctx",
  unknown: "Unknown",
};

const KIND_ORDER = ["plan", "read_only", "edit", "test_run", "housekeeping", "subagent", "long_context", "unknown"];

function kindLabel(k: string) {
  return KIND_LABELS[k] || k;
}

function ModelFitContent({
  evidence,
  winLbl,
}: {
  evidence: EvidenceMatrixResponse;
  winLbl: string;
}) {
  const cells = evidence.cells;

  const allKinds = useMemo(() => {
    const set = new Set<string>();
    cells.forEach((c) => set.add(c.kind));
    return Array.from(set).sort((a, b) => {
      const ia = KIND_ORDER.indexOf(a);
      const ib = KIND_ORDER.indexOf(b);
      if (ia >= 0 && ib >= 0) return ia - ib;
      if (ia >= 0) return -1;
      if (ib >= 0) return 1;
      return a.localeCompare(b);
    });
  }, [cells]);

  const bestPerKind = useMemo(() => {
    const out: Record<string, { model: string; provider: string; costPerTurn: number; turns: number }> = {};
    cells.forEach((c) => {
      if (c.turns === 0) return;
      const cpt = c.cost_usd / c.turns;
      const existing = out[c.kind];
      if (!existing || cpt < existing.costPerTurn) {
        out[c.kind] = { model: c.model, provider: c.provider || "unknown", costPerTurn: cpt, turns: c.turns };
      }
    });
    return out;
  }, [cells]);

  const totalTurns = cells.reduce((sum, c) => sum + c.turns, 0);
  const totalCost = cells.reduce((sum, c) => sum + c.cost_usd, 0);
  const totalErrors = cells.reduce((sum, c) => sum + c.error_rate * c.turns, 0);
  const avgErrorRate = totalTurns > 0 ? totalErrors / totalTurns : 0;

  return (
    <div className="space-y-6 p-6">
      <PageHeader icon={navIcon("modelfit")}
        title="Model Fit"
        sub="Best model per requirement kind - evidence from your history. Cells show cost/turn, error rate, latency; greener = better fit."
        right={
          <div className="flex items-center gap-2">
            <Pill>
              {winLbl} · {cells.length} kind×model×provider combos
            </Pill>
          </div>
        }
      />

      {/* KPI chips */}
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-5">
        <StatCard label="Total turns" value={fmtInt(totalTurns)} />
        <StatCard label="Total cost" value={fmtUSD(totalCost)} unit="USD" accent />
        <StatCard label="Avg cost/turn" value={totalTurns > 0 ? fmtUSD(totalCost / totalTurns) : "$0.00"} />
        <StatCard label="Error rate" value={fmtPct(avgErrorRate)} warn={avgErrorRate > 0.05} />
        <StatCard label="Kinds covered" value={fmtInt(allKinds.length)} />
      </div>

      {/* Best model per kind cards */}
      <section className="space-y-2">
        <h2 className="mb-2 text-[12px] font-medium uppercase tracking-wide text-fg-3">
          Best model per kind (lowest cost/turn)
        </h2>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {allKinds.map((kind) => {
            const best = bestPerKind[kind];
            if (!best) return null;
            return (
              <div
                key={kind}
                className="rounded-3 border border-line-2 bg-bg-2 px-4 py-3"
              >
                <div className="flex items-center justify-between">
                  <div>
                    <Pill variant="accent">{kindLabel(kind)}</Pill>
                    <div className="mt-1 text-[13px] font-medium text-fg-1">{best.model}</div>
                    <div className="text-[11px] text-fg-3">{best.provider}</div>
                  </div>
                  <div className="text-right">
                    <div className="text-[14px] font-semibold text-fg-1">
                      {fmtUSD(best.costPerTurn)}/turn
                    </div>
                    <div className="text-[11px] text-fg-3">{fmtInt(best.turns)} turns</div>
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      </section>

      {/* Full matrix table */}
      <section>
        <h2 className="mb-2 text-[12px] font-medium uppercase tracking-wide text-fg-3">
          Full evidence matrix (kind × model × provider)
        </h2>
        <div className="overflow-hidden rounded-3 border border-line-2">
          <table className="w-full text-left text-[12px]">
            <thead className="bg-bg-2 text-[11px] uppercase tracking-wide text-fg-3">
              <tr>
                <th className="px-3 py-2">Kind</th>
                <th className="px-3 py-2">Model</th>
                <th className="px-3 py-2">Provider</th>
                <th className="px-3 py-2 text-right">Turns</th>
                <th className="px-3 py-2 text-right">Cost/turn</th>
                <th className="px-3 py-2 text-right">Total cost</th>
                <th className="px-3 py-2 text-right">Error rate</th>
                <th className="px-3 py-2 text-right">Avg TTFT</th>
                <th className="px-3 py-2 text-right">Avg latency</th>
                <th className="px-3 py-2 text-right">Cache read</th>
                <th className="px-3 py-2 text-right">Cache write</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line-1 bg-bg-1">
              {cells
                .sort((a, b) => {
                  const ka = KIND_ORDER.indexOf(a.kind);
                  const kb = KIND_ORDER.indexOf(b.kind);
                  if (ka !== kb) return ka - kb;
                  if (a.model !== b.model) return a.model.localeCompare(b.model);
                  return (a.provider || "").localeCompare(b.provider || "");
                })
                .map((c) => (
                  <tr key={`${c.kind}|${c.model}|${c.provider}`}>
                    <td className="px-3 py-2 font-medium text-fg-1">
                      <Pill variant={c.kind === "unknown" ? "neutral" : "accent"}>
                        {kindLabel(c.kind)}
                      </Pill>
                    </td>
                    <td className="px-3 py-2 font-mono text-[11px] text-fg-1">{c.model}</td>
                    <td className="px-3 py-2 text-fg-2">{c.provider || "unknown"}</td>
                    <td className="px-3 py-2 text-right text-fg-2">{fmtInt(c.turns)}</td>
                    <td className="px-3 py-2 text-right text-fg-1">
                      {c.turns > 0 ? fmtUSD(c.cost_usd / c.turns) : " - "}
                    </td>
                    <td className="px-3 py-2 text-right text-fg-1">{fmtUSD(c.cost_usd)}</td>
                    <td className="px-3 py-2 text-right">
                      {c.error_rate > 0.1 ? (
                        <span className="text-warn">{fmtPct(c.error_rate)}</span>
                      ) : (
                        <span className="text-fg-2">{fmtPct(c.error_rate)}</span>
                      )}
                    </td>
                    <td className="px-3 py-2 text-right text-fg-2">
                      {c.avg_ttft_ms > 0 ? fmtInt(Math.round(c.avg_ttft_ms)) + "ms" : " - "}
                    </td>
                    <td className="px-3 py-2 text-right text-fg-2">
                      {c.avg_total_ms > 0 ? fmtInt(Math.round(c.avg_total_ms)) + "ms" : " - "}
                    </td>
                    <td className="px-3 py-2 text-right text-fg-2">{fmtCompact(c.cache_read_tokens)}</td>
                    <td className="px-3 py-2 text-right text-fg-2">{fmtCompact(c.cache_creation_tokens)}</td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
      </section>

      {/* Legend / guidance */}
      <section className="rounded-3 border border-line-2 bg-bg-2 px-4 py-3">
        <h3 className="mb-2 text-[12px] font-medium text-fg-1">How to read this</h3>
        <ul className="space-y-1 text-[12px] text-fg-2">
          <li>
            <strong>Cost/turn</strong> = total USD ÷ turns. Lower is more cost-efficient for this kind.
          </li>
          <li>
            <strong>Error rate</strong> = turns with error_class ÷ total turns. {'>'}5% warrants investigation.
          </li>
          <li>
            <strong>TTFT / Latency</strong> = average from api_turns (ms). Lower = snappier.
          </li>
          <li>
            <strong>Cache read/write</strong> = prompt-cache utilization. High read = good; high write without read = churn.
          </li>
          <li>
            The "Best model per kind" cards above pick the model/provider with the lowest cost/turn for each
            requirement kind (minimum 1 turn). This is a starting point - combine with your quality requirements.
          </li>
        </ul>
      </section>
    </div>
  );
}

export function ModelFitPage() {
  const { win, customRange, tool, project } = useFilters();
  const winParams = windowParams(win, customRange);
  const winLbl = windowLabel(win, customRange);
  const projectParam = project === "all" ? undefined : project;
  const toolParam = tool === "all" ? undefined : tool;

  const evidence = useApi<EvidenceMatrixResponse>(
    "/api/evidence/matrix",
    { ...winParams, tool: toolParam, project: projectParam },
    [win, customRange, tool, project],
  );

  if (evidence.error) {
    return (
      <div className="p-6">
        <PageHeader icon={navIcon("modelfit")}
          title="Model Fit"
          sub="Best model per requirement kind - evidence from your history."
        />
        <div className="mt-4 rounded-3 border border-warn/30 bg-warn/5 px-4 py-3 text-[12px] text-warn">
          Evidence unavailable: {evidence.error.message}
        </div>
      </div>
    );
  }

  if (!evidence.data) {
    return (
      <div className="p-6">
        <PageHeader icon={navIcon("modelfit")}
          title="Model Fit"
          sub="Best model per requirement kind - evidence from your history."
        />
        <div className="mt-4 text-[12px] text-fg-3">Loading…</div>
      </div>
    );
  }

  return <ModelFitContent evidence={evidence.data} winLbl={winLbl} />;
}
