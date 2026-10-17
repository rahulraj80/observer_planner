import { useMemo, useState } from "react";
import {
  ChartShell,
  PageHeader,
  Pill,
  StatCard,
  Tooltip,
} from "@/components/primitives";
import { useFilters, windowParams, windowLabel, windowSpanHours } from "@/lib/filters";
import { useApi } from "@/lib/useApi";
import { fmtCompact, fmtInt, fmtPct, fmtUSD } from "@/lib/format";
import type { CostSummary } from "@/lib/types";
import { navIcon } from "@/lib/nav";

interface EvidenceTimeseriesPoint {
  bucket: string;
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

interface EvidenceTimeseriesResponse {
  days: number;
  since: string;
  until: string;
  bucket: string;
  points: EvidenceTimeseriesPoint[];
}

interface EvidenceSummaryItem {
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
}

interface EvidenceSummaryResponse {
  days: number;
  since: string;
  until: string;
  items: EvidenceSummaryItem[];
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

function kindLabel(k: string) {
  return KIND_LABELS[k] || k;
}

export function APIExplorerPage() {
  const { win, customRange, tool, project } = useFilters();
  const [provider, setProvider] = useState("all");
  const [kind, setKind] = useState("all");
  const [model, setModel] = useState("all");
  const winParams = windowParams(win, customRange);
  const bucket = windowSpanHours(win, customRange) <= 48 ? "hour" : "day";
  const winLbl = windowLabel(win, customRange);
  const projectParam = project === "all" ? undefined : project;
  const toolParam = tool === "all" ? undefined : tool;
  const providerParam = provider === "all" ? undefined : provider;
  const kindParam = kind === "all" ? undefined : kind;
  const modelParam = model === "all" ? undefined : model;

  const evidenceSummary = useApi<EvidenceSummaryResponse>(
    "/api/evidence/summary",
    { ...winParams, tool: toolParam, project: projectParam },
    [win, customRange, tool, project],
  );

  const evidenceTs = useApi<EvidenceTimeseriesResponse>(
    "/api/evidence/timeseries",
    {
      ...winParams,
      kind: kindParam,
      model: modelParam,
      provider: providerParam,
      bucket,
    },
    [win, customRange, kind, model, provider],
  );

  const models = useApi<CostSummary>(
    "/api/models",
    { ...winParams, tool: toolParam, project: projectParam },
    [win, customRange, tool, project],
  );

  const allKinds = useMemo(() => {
    const set = new Set<string>();
    evidenceSummary.data?.items.forEach((i) => set.add(i.kind));
    return Array.from(set).sort();
  }, [evidenceSummary.data]);

  const allModels = useMemo(() => {
    const set = new Set<string>();
    evidenceSummary.data?.items.forEach((i) => set.add(i.model));
    models.data?.rows.forEach((r) => set.add(r.key));
    return Array.from(set).sort();
  }, [evidenceSummary.data, models.data]);

  const allProviders = useMemo(() => {
    const set = new Set<string>();
    evidenceSummary.data?.items.forEach((i) => set.add(i.provider || "unknown"));
    return Array.from(set).sort();
  }, [evidenceSummary.data]);

  if (evidenceSummary.error || evidenceTs.error) {
    return (
      <div className="p-6">
        <PageHeader icon={navIcon("apiexplorer")}
          title="API Explorer"
          sub="Provider × model × kind × time cuts - your API usage under the microscope."
        />
        <div className="mt-4 rounded-3 border border-warn/30 bg-warn/5 px-4 py-3 text-[12px] text-warn">
          Evidence unavailable: {evidenceSummary.error?.message || evidenceTs.error?.message}
        </div>
      </div>
    );
  }

  if (!evidenceSummary.data || !evidenceTs.data) {
    return (
      <div className="p-6">
        <PageHeader icon={navIcon("apiexplorer")}
          title="API Explorer"
          sub="Provider × model × kind × time cuts - your API usage under the microscope."
        />
        <div className="mt-4 text-[12px] text-fg-3">Loading…</div>
      </div>
    );
  }

  const items = evidenceSummary.data.items;
  const totalTurns = items.reduce((sum, i) => sum + i.turns, 0);
  const totalCost = items.reduce((sum, i) => sum + i.cost_usd, 0);
  const totalInput = items.reduce((sum, i) => sum + i.input_tokens, 0);
  const totalOutput = items.reduce((sum, i) => sum + i.output_tokens, 0);
  const totalCacheRead = items.reduce((sum, i) => sum + i.cache_read_tokens, 0);
  const totalCacheWrite = items.reduce((sum, i) => sum + i.cache_creation_tokens, 0);

  return (
    <div className="space-y-6 p-6">
      <PageHeader icon={navIcon("apiexplorer")}
        title="API Explorer"
        sub="Provider × model × kind × time cuts - your API usage under the microscope."
        right={
          <div className="flex items-center gap-2">
            <Pill>{winLbl} · bucket: {bucket}</Pill>
          </div>
        }
      />

      {/* Filters row */}
      <section className="flex flex-wrap gap-2">
        <Tooltip content="Filter by provider">
          <select
            value={provider}
            onChange={(e) => setProvider(e.target.value)}
            className="rounded-3 border border-line-2 bg-bg-2 px-2 py-1 text-[12px] text-fg-1"
          >
            <option value="all">All providers</option>
            {allProviders.map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </select>
        </Tooltip>
        <Tooltip content="Filter by requirement kind">
          <select
            value={kind}
            onChange={(e) => setKind(e.target.value)}
            className="rounded-3 border border-line-2 bg-bg-2 px-2 py-1 text-[12px] text-fg-1"
          >
            <option value="all">All kinds</option>
            {allKinds.map((k) => (
              <option key={k} value={k}>
                {kindLabel(k)}
              </option>
            ))}
          </select>
        </Tooltip>
        <Tooltip content="Filter by model">
          <select
            value={model}
            onChange={(e) => setModel(e.target.value)}
            className="rounded-3 border border-line-2 bg-bg-2 px-2 py-1 text-[12px] text-fg-1"
          >
            <option value="all">All models</option>
            {allModels.map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </select>
        </Tooltip>
      </section>

      {/* KPI chips */}
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-6">
        <StatCard label="Turns" value={fmtInt(totalTurns)} />
        <StatCard label="Cost" value={fmtUSD(totalCost)} unit="USD" accent />
        <StatCard label="Input tokens" value={fmtCompact(totalInput)} />
        <StatCard label="Output tokens" value={fmtCompact(totalOutput)} />
        <StatCard label="Cache read" value={fmtCompact(totalCacheRead)} />
        <StatCard label="Cache write" value={fmtCompact(totalCacheWrite)} />
      </div>

      {/* Timeseries chart area - cost + tokens */}
      <section>
        <h2 className="mb-2 text-[12px] font-medium uppercase tracking-wide text-fg-3">
          Cost & tokens over time
        </h2>
        <ChartShell title="Cost & tokens over time">
          <div className="text-center text-fg-3 text-[12px] py-8">
            Timeseries chart placeholder - connect recharts for evidenceTs
          </div>
        </ChartShell>
      </section>

      {/* Provider breakdown table */}
      <section>
        <h2 className="mb-2 text-[12px] font-medium uppercase tracking-wide text-fg-3">
          Provider × Model breakdown
        </h2>
        <div className="overflow-hidden rounded-3 border border-line-2">
          <table className="w-full text-left text-[12px]">
            <thead className="bg-bg-2 text-[11px] uppercase tracking-wide text-fg-3">
              <tr>
                <th className="px-3 py-2">Provider</th>
                <th className="px-3 py-2">Model</th>
                <th className="px-3 py-2">Kind</th>
                <th className="px-3 py-2 text-right">Turns</th>
                <th className="px-3 py-2 text-right">Cost</th>
                <th className="px-3 py-2 text-right">Input tok</th>
                <th className="px-3 py-2 text-right">Output tok</th>
                <th className="px-3 py-2 text-right">Cache read</th>
                <th className="px-3 py-2 text-right">Cache write</th>
                <th className="px-3 py-2 text-right">Error rate</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line-1 bg-bg-1">
              {items
                .filter((i) => {
                  if (kindParam && i.kind !== kindParam) return false;
                  if (modelParam && i.model !== modelParam) return false;
                  if (providerParam && (i.provider || "unknown") !== providerParam) return false;
                  return true;
                })
                .sort((a, b) => b.cost_usd - a.cost_usd)
                .map((i) => (
                  <tr key={`${i.provider}|${i.model}|${i.kind}`}>
                    <td className="px-3 py-2 text-fg-2">{i.provider || "unknown"}</td>
                    <td className="px-3 py-2 font-mono text-[11px] text-fg-1">{i.model}</td>
                    <td className="px-3 py-2">
                      <Pill variant="accent">
                        {kindLabel(i.kind)}
                      </Pill>
                    </td>
                    <td className="px-3 py-2 text-right text-fg-2">{fmtInt(i.turns)}</td>
                    <td className="px-3 py-2 text-right text-fg-1">{fmtUSD(i.cost_usd)}</td>
                    <td className="px-3 py-2 text-right text-fg-2">{fmtCompact(i.input_tokens)}</td>
                    <td className="px-3 py-2 text-right text-fg-2">{fmtCompact(i.output_tokens)}</td>
                    <td className="px-3 py-2 text-right text-fg-2">{fmtCompact(i.cache_read_tokens)}</td>
                    <td className="px-3 py-2 text-right text-fg-2">{fmtCompact(i.cache_creation_tokens)}</td>
                    <td className="px-3 py-2 text-right">
                      {i.error_rate > 0.1 ? (
                        <span className="text-warn">{fmtPct(i.error_rate)}</span>
                      ) : (
                        <span className="text-fg-2">{fmtPct(i.error_rate)}</span>
                      )}
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
      </section>

      {/* Timeseries detail for selected filter */}
      <section>
        <h2 className="mb-2 text-[12px] font-medium uppercase tracking-wide text-fg-3">
          Time-series detail ({bucket} buckets)
        </h2>
        <div className="overflow-hidden rounded-3 border border-line-2">
          <table className="w-full text-left text-[12px]">
            <thead className="bg-bg-2 text-[11px] uppercase tracking-wide text-fg-3">
              <tr>
                <th className="px-3 py-2">Bucket</th>
                <th className="px-3 py-2 text-right">Turns</th>
                <th className="px-3 py-2 text-right">Cost</th>
                <th className="px-3 py-2 text-right">Input tok</th>
                <th className="px-3 py-2 text-right">Output tok</th>
                <th className="px-3 py-2 text-right">Cache read</th>
                <th className="px-3 py-2 text-right">Cache write</th>
                <th className="px-3 py-2 text-right">Error rate</th>
                <th className="px-3 py-2 text-right">Avg TTFT</th>
                <th className="px-3 py-2 text-right">Avg latency</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line-1 bg-bg-1">
              {evidenceTs.data.points
                .sort((a, b) => a.bucket.localeCompare(b.bucket))
                .map((p) => (
                  <tr key={p.bucket}>
                    <td className="px-3 py-2 font-mono text-[11px] text-fg-1">{p.bucket}</td>
                    <td className="px-3 py-2 text-right text-fg-2">{fmtInt(p.turns)}</td>
                    <td className="px-3 py-2 text-right text-fg-1">{fmtUSD(p.cost_usd)}</td>
                    <td className="px-3 py-2 text-right text-fg-2">{fmtCompact(p.input_tokens)}</td>
                    <td className="px-3 py-2 text-right text-fg-2">{fmtCompact(p.output_tokens)}</td>
                    <td className="px-3 py-2 text-right text-fg-2">{fmtCompact(p.cache_read_tokens)}</td>
                    <td className="px-3 py-2 text-right text-fg-2">{fmtCompact(p.cache_creation_tokens)}</td>
                    <td className="px-3 py-2 text-right">
                      {p.error_rate > 0.1 ? (
                        <span className="text-warn">{fmtPct(p.error_rate)}</span>
                      ) : (
                        <span className="text-fg-2">{fmtPct(p.error_rate)}</span>
                      )}
                    </td>
                    <td className="px-3 py-2 text-right text-fg-2">
                      {p.avg_ttft_ms > 0 ? fmtInt(Math.round(p.avg_ttft_ms)) + "ms" : " - "}
                    </td>
                    <td className="px-3 py-2 text-right text-fg-2">
                      {p.avg_total_ms > 0 ? fmtInt(Math.round(p.avg_total_ms)) + "ms" : " - "}
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}