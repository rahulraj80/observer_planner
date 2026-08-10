import { useMemo } from "react";
import {
  PageHeader,
  Pill,
  StatCard,
  Tooltip,
} from "@/components/primitives";
import { useApi } from "@/lib/useApi";
import { fmtCompact, fmtInt, fmtUSD } from "@/lib/format";

interface LicenseUsageRow {
  license_id: string;
  observed_at: string;
  entitlement_kind: string;
  provider: string;
  models_used_json: string;
  turns: number;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_creation_tokens: number;
  cost_usd_list: number;
  quota_monthly_tokens: number | null;
}

interface LicenseUsageResponse {
  days: number;
  since: string;
  until: string;
  items: LicenseUsageRow[];
}

const KIND_LABELS: Record<string, string> = {
  subscription: "Subscription",
  api: "Pay-per-token",
  plan: "Plan",
};

function kindLabel(k: string) {
  return KIND_LABELS[k] || k;
}

function LicensesContent({
  licenseUsage,
}: {
  licenseUsage: LicenseUsageResponse;
}) {
  const items = licenseUsage.items;

  // Aggregate by license
  const byLicense = useMemo(() => {
    const m = new Map<string, {
      license_id: string;
      entitlement_kind: string;
      provider: string;
      models: Set<string>;
      totalTurns: number;
      totalInputTokens: number;
      totalOutputTokens: number;
      totalCacheRead: number;
      totalCacheWrite: number;
      totalCostUSD: number;
      quotaMonthlyTokens: number | null;
    }>();
    items.forEach((i) => {
      let entry = m.get(i.license_id);
      if (!entry) {
        entry = {
          license_id: i.license_id,
          entitlement_kind: i.entitlement_kind,
          provider: i.provider,
          models: new Set(),
          totalTurns: 0,
          totalInputTokens: 0,
          totalOutputTokens: 0,
          totalCacheRead: 0,
          totalCacheWrite: 0,
          totalCostUSD: 0,
          quotaMonthlyTokens: i.quota_monthly_tokens,
        };
        m.set(i.license_id, entry);
      }
      try {
        const models = JSON.parse(i.models_used_json);
        models.forEach((mod: string) => entry!.models.add(mod));
      } catch {
        // ignore
      }
      entry.totalTurns += i.turns;
      entry.totalInputTokens += i.input_tokens;
      entry.totalOutputTokens += i.output_tokens;
      entry.totalCacheRead += i.cache_read_tokens;
      entry.totalCacheWrite += i.cache_creation_tokens;
      entry.totalCostUSD += i.cost_usd_list;
      if (i.quota_monthly_tokens && entry.quotaMonthlyTokens === null) {
        entry.quotaMonthlyTokens = i.quota_monthly_tokens;
      }
    });
    return Array.from(m.values());
  }, [items]);

  const totalTurns = items.reduce((sum, i) => sum + i.turns, 0);
  const totalCost = items.reduce((sum, i) => sum + i.cost_usd_list, 0);

  return (
    <div className="space-y-6 p-6">
      <PageHeader
        title="Licenses & Plans"
        sub="Your license inventory, utilization, and break-even analysis. Configure licenses in [licenses] config."
        right={
          <Pill>
            {items.length} hourly buckets
          </Pill>
        }
      />

      {/* KPI chips */}
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-5">
        <StatCard label="Licenses tracked" value={fmtInt(byLicense.length)} />
        <StatCard label="Total turns" value={fmtInt(totalTurns)} />
        <StatCard label="Total cost (list)" value={fmtUSD(totalCost)} unit="USD" accent />
        <StatCard label="Avg cost/turn" value={totalTurns > 0 ? fmtUSD(totalCost / totalTurns) : "$0.00"} />
      </div>

      {/* License cards */}
      <section>
        <h2 className="mb-2 text-[12px] font-medium uppercase tracking-wide text-fg-3">
          License inventory
        </h2>
        {byLicense.length === 0 ? (
          <div className="rounded-3 border border-line-2 bg-bg-2 px-4 py-6 text-center">
            <p className="text-fg-3 mb-2">No license usage data yet.</p>
            <p className="text-[12px] text-fg-2">
              Configure licenses in <code className="bg-bg-1 px-1 rounded">config.toml</code>
              under the <code className="bg-bg-1 px-1 rounded">[licenses]</code> section, then
              generate some API traffic (via proxy or watcher) to populate the usage table.
            </p>
          </div>
        ) : (
          <div className="space-y-3">
            {byLicense.map((lic) => {
              const quotaUsed = lic.totalInputTokens + lic.totalOutputTokens;
              const quotaPct = lic.quotaMonthlyTokens && lic.quotaMonthlyTokens > 0
                ? quotaUsed / lic.quotaMonthlyTokens
                : 0;
              return (
                <div
                  key={lic.license_id}
                  className="rounded-3 border border-line-2 bg-bg-2 px-4 py-3"
                >
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <div className="flex items-center gap-3">
                      <Pill variant={lic.entitlement_kind === "subscription" ? "accent" : "neutral"}>
                        {kindLabel(lic.entitlement_kind)}
                      </Pill>
                      <div>
                        <div className="text-[13px] font-medium text-fg-1">{lic.license_id}</div>
                        <div className="text-[11px] text-fg-3">
                          {lic.provider} · {lic.models.size} models
                        </div>
                      </div>
                    </div>
                    <div className="flex items-center gap-4 text-right">
                      <div>
                        <div className="text-[12px] font-medium text-fg-1">{fmtUSD(lic.totalCostUSD)}</div>
                        <div className="text-[11px] text-fg-3">list cost</div>
                      </div>
                      <div>
                        <div className="text-[12px] font-medium text-fg-1">{fmtInt(lic.totalTurns)}</div>
                        <div className="text-[11px] text-fg-3">turns</div>
                      </div>
                      <div>
                        <div className="text-[12px] font-medium text-fg-1">
                          {fmtCompact(quotaUsed)} / {lic.quotaMonthlyTokens ? fmtCompact(lic.quotaMonthlyTokens) : "∞"}
                        </div>
                        <div className="text-[11px] text-fg-3">quota used</div>
                      </div>
                      {lic.quotaMonthlyTokens && lic.quotaMonthlyTokens > 0 && (
                        <Tooltip content={`${Math.round(quotaPct * 100)}% of monthly quota used`}>
                          <div className="w-32">
                            <div className="h-2 rounded-full bg-bg-1 overflow-hidden">
                              <div
                                className="h-full bg-accent transition-all"
                                style={{ width: `${Math.min(100, quotaPct * 100)}%` }}
                              />
                            </div>
                            <div className="text-[10px] text-fg-3 mt-0.5 text-right">
                              {Math.round(quotaPct * 100)}%
                            </div>
                          </div>
                        </Tooltip>
                      )}
                    </div>
                  </div>
                  <div className="mt-2 text-[11px] text-fg-3">
                    Models: {Array.from(lic.models).slice(0, 5).join(", ")}
                    {lic.models.size > 5 && <span> +{lic.models.size - 5} more</span>}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </section>

      {/* Raw hourly buckets */}
      <section>
        <h2 className="mb-2 text-[12px] font-medium uppercase tracking-wide text-fg-3">
          Hourly usage buckets
        </h2>
        <div className="overflow-hidden rounded-3 border border-line-2">
          <table className="w-full text-left text-[12px]">
            <thead className="bg-bg-2 text-[11px] uppercase tracking-wide text-fg-3">
              <tr>
                <th className="px-3 py-2">Time (UTC)</th>
                <th className="px-3 py-2">License</th>
                <th className="px-3 py-2">Kind</th>
                <th className="px-3 py-2">Provider</th>
                <th className="px-3 py-2 text-right">Turns</th>
                <th className="px-3 py-2 text-right">Cost (list)</th>
                <th className="px-3 py-2 text-right">Input tok</th>
                <th className="px-3 py-2 text-right">Output tok</th>
                <th className="px-3 py-2 text-right">Cache R/W</th>
                <th className="px-3 py-2 text-right">Quota</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line-1 bg-bg-1">
              {items
                .sort((a, b) => b.observed_at.localeCompare(a.observed_at))
                .map((i) => (
                  <tr key={`${i.license_id}|${i.observed_at}`}>
                    <td className="px-3 py-2 font-mono text-[11px] text-fg-1">{i.observed_at.replace("T", " ").substring(0, 16)}</td>
                    <td className="px-3 py-2 text-fg-2">{i.license_id}</td>
                    <td className="px-3 py-2">
                      <Pill variant={i.entitlement_kind === "subscription" ? "accent" : "neutral"}>
                        {kindLabel(i.entitlement_kind)}
                      </Pill>
                    </td>
                    <td className="px-3 py-2 text-fg-2">{i.provider}</td>
                    <td className="px-3 py-2 text-right text-fg-2">{fmtInt(i.turns)}</td>
                    <td className="px-3 py-2 text-right text-fg-1">{fmtUSD(i.cost_usd_list)}</td>
                    <td className="px-3 py-2 text-right text-fg-2">{fmtCompact(i.input_tokens)}</td>
                    <td className="px-3 py-2 text-right text-fg-2">{fmtCompact(i.output_tokens)}</td>
                    <td className="px-3 py-2 text-right text-fg-2">{fmtCompact(i.cache_read_tokens)} / {fmtCompact(i.cache_creation_tokens)}</td>
                    <td className="px-3 py-2 text-right text-fg-2">
                      {i.quota_monthly_tokens ? fmtCompact(i.quota_monthly_tokens) : "—"}
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

export function LicensesPage() {
  const licenseUsage = useApi<LicenseUsageResponse>(
    "/api/license/usage",
    undefined,
    [],
  );

  if (licenseUsage.error) {
    return (
      <div className="p-6">
        <PageHeader
          title="Licenses & Plans"
          sub="Your license inventory, utilization, and break-even analysis."
        />
        <div className="mt-4 rounded-3 border border-warn/30 bg-warn/5 px-4 py-3 text-[12px] text-warn">
          License data unavailable: {licenseUsage.error.message}
        </div>
      </div>
    );
  }

  if (!licenseUsage.data) {
    return (
      <div className="p-6">
        <PageHeader
          title="Licenses & Plans"
          sub="Your license inventory, utilization, and break-even analysis."
        />
        <div className="mt-4 text-[12px] text-fg-3">Loading…</div>
      </div>
    );
  }

  return <LicensesContent licenseUsage={licenseUsage.data} />;
}