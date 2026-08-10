import { useEffect, useState } from "react";
import { api, type BriefingAggregate, type OrgBriefing } from "@/lib/api";
import { useApi } from "@/lib/useApi";
import { compact, num, pct1, usd } from "@/lib/format";
import { Badge, Card, Empty, ErrorState, PageHeader, Spinner } from "@/components/ui";

// Org daily briefing (Phase C of docs/design/standup-briefing.md §4). The
// server composes the org's day into the SAME pure internal/briefing document
// the node dashboard uses — KPI chips, per-tool activity, top sessions, and
// opportunity cards (drill-down math embedded in the payload, so no separate
// justify endpoint). Role-scoped: leads see their teams, admins the whole org.
// Admins also get the pushed hash-only aggregate digest beneath the briefing.

export function BriefingPage() {
  const [date, setDate] = useState("");
  const { data, error, loading, reload } = useApi(() => api.briefing(date || undefined), [date]);
  // Admin-only aggregates feed; hidden silently for non-admins (403).
  const [aggregates, setAggregates] = useState<BriefingAggregate[] | null>(null);

  const b = data;
  useEffect(() => {
    if (b && !date) setDate(b.date);
  }, [b, date]);

  useEffect(() => {
    if (!date) return;
    let live = true;
    api
      .briefingAggregates(date)
      .then((r) => {
        if (live) setAggregates(r.aggregates);
      })
      .catch(() => {
        if (live) setAggregates(null);
      });
    return () => {
      live = false;
    };
  }, [date]);

  return (
    <>
      <PageHeader
        title="Daily briefing"
        subtitle="Yesterday's org story: what ran across every enrolled node, what it cost, and what the data says you could change. Content-free — counts, tokens, and costs only, never prompts or file content."
        right={
          <div className="flex items-center gap-2">
            <input
              type="date"
              value={date}
              onChange={(e) => setDate(e.target.value)}
              className="rounded border border-line bg-surface2 px-2 py-1 text-xs text-fg focus:border-accent focus:outline-none"
              aria-label="Briefing date"
            />
            {b && <Badge tone="accent">{b.basis} pricing</Badge>}
          </div>
        }
      />
      {error ? (
        <ErrorState message={error} onRetry={reload} />
      ) : loading || !b ? (
        <Card className="p-4">
          <Spinner label="Composing briefing…" />
        </Card>
      ) : (
        <div className="space-y-5">
          <Narrative text={b.narrative} generatedAt={b.generated_at} />
          <KPIGrid b={b} />
          <ActivityTable rows={b.activity} />
          {b.top_sessions.length > 0 && <TopSessions rows={b.top_sessions} />}
          <Opportunities rows={b.opportunities} />
          {aggregates && aggregates.length > 0 && <AggregatesTable rows={aggregates} />}
          {b.questions.length > 0 && (
            <Card className="p-3">
              <div className="mb-2 px-1 text-sm font-medium text-fg-1">Open questions</div>
              <ul className="space-y-1 text-xs text-fg-2">
                {b.questions.map((q, i) => (
                  <li key={i} className="flex gap-2">
                    <span className="text-accent">·</span>
                    <span>{q}</span>
                  </li>
                ))}
              </ul>
            </Card>
          )}
        </div>
      )}
    </>
  );
}

function Narrative({ text, generatedAt }: { text: string; generatedAt: string }) {
  return (
    <Card className="border-accent/30">
      <div className="flex items-start gap-3">
        <span className="mt-0.5 shrink-0 rounded bg-accent/15 px-1.5 py-0.5 font-mono text-[10px] text-accent">
          brief
        </span>
        <div>
          <p className="text-sm leading-relaxed text-fg-1">{text}</p>
          <p className="mt-1 text-[11px] text-faint">Generated {new Date(generatedAt).toLocaleString("en-US")}</p>
        </div>
      </div>
    </Card>
  );
}

function KPIGrid({ b }: { b: OrgBriefing }) {
  const g = b.at_glance;
  const kpis: { label: string; value: string; accent?: boolean; warn?: boolean }[] = [
    { label: "Spend", value: usd(g.spend_usd), accent: true },
    { label: "Sessions", value: num(g.sessions) },
    { label: "Turns", value: num(g.turns) },
    { label: "Active hours", value: num(Math.round(g.active_hours)) },
    { label: "Cache hit rate", value: pct1(g.cache_hit_rate), warn: g.cache_hit_rate < 0.6 },
    { label: "Error turns", value: num(g.error_turns), warn: g.error_turns > 0 },
    { label: "Input tokens", value: compact(g.input_tokens) },
    { label: "Output tokens", value: compact(g.output_tokens) },
    { label: "Cache read", value: compact(g.cache_read_tokens) },
    { label: "Cache created", value: compact(g.cache_creation_tokens) },
    { label: "Fast turns", value: pct1(g.fast_share) },
  ];
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
      {kpis.map((k) => (
        <Card key={k.label} className={k.accent ? "border-accent/40" : k.warn ? "border-bad/40" : ""}>
          <div className="text-[11px] uppercase tracking-wide text-faint">{k.label}</div>
          <div className={`mt-1 font-mono text-lg leading-tight ${k.warn ? "text-bad" : "text-fg"}`}>{k.value}</div>
        </Card>
      ))}
    </div>
  );
}

function ActivityTable({ rows }: { rows: OrgBriefing["activity"] }) {
  return (
    <Card className="p-3">
      <div className="mb-2 px-1 text-sm font-medium text-fg-1">Activity by tool</div>
      {rows.length === 0 ? (
        <Empty message="No tool activity in this window." />
      ) : (
        <table className="w-full text-left text-xs">
          <thead className="text-[10px] uppercase tracking-[0.06em] text-faint">
            <tr className="border-b border-line">
              <th className="bg-surface px-2 py-2 font-medium">Tool</th>
              <th className="bg-surface px-2 py-2 text-right font-medium">Sessions</th>
              <th className="bg-surface px-2 py-2 text-right font-medium">Turns</th>
              <th className="bg-surface px-2 py-2 text-right font-medium">Spend</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((a) => (
              <tr key={a.tool} className="border-b border-line last:border-b-0">
                <td className="px-2 py-1.5 font-medium text-fg-1">{a.tool}</td>
                <td className="px-2 py-1.5 text-right text-fg-2">{num(a.sessions)}</td>
                <td className="px-2 py-1.5 text-right text-fg-2">{num(a.turns)}</td>
                <td className="whitespace-nowrap px-2 py-1.5 text-right font-mono tabular-nums text-fg-1">
                  {usd(a.spend_usd)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Card>
  );
}

function TopSessions({ rows }: { rows: OrgBriefing["top_sessions"] }) {
  return (
    <Card className="p-3">
      <div className="mb-2 px-1 text-sm font-medium text-fg-1">Top sessions by spend</div>
      <div className="space-y-1">
        {rows.map((s) => (
          <div key={s.id} className="flex items-center justify-between gap-3 rounded border border-line bg-surface2 px-3 py-2">
            <div className="flex min-w-0 items-center gap-2">
              <span className="truncate font-mono text-xs text-fg-1">{s.id}</span>
              {s.tool && <Badge>{s.tool}</Badge>}
              <span className="shrink-0 text-[11px] text-faint">{num(s.turns)} turns</span>
            </div>
            <span className="shrink-0 font-mono text-xs font-medium text-fg">{usd(s.cost_usd)}</span>
          </div>
        ))}
      </div>
    </Card>
  );
}

function severityBadge(severity: string) {
  switch (severity) {
    case "suggestion":
      return <Badge tone="accent">suggestion</Badge>;
    case "warning":
      return <Badge tone="warn">warning</Badge>;
    default:
      return <Badge>info</Badge>;
  }
}

function Opportunities({ rows }: { rows: OrgBriefing["opportunities"] }) {
  const [openKey, setOpenKey] = useState<string | null>(null);
  return (
    <Card className="p-3">
      <div className="mb-2 px-1 text-sm font-medium text-fg-1">Opportunities</div>
      {rows.length === 0 ? (
        <Empty message="Nothing above the reporting threshold. Good day." />
      ) : (
        <div className="space-y-2">
          {rows.map((o) => (
            <div key={o.dedup_key} className="rounded border border-line bg-surface2">
              <button
                type="button"
                onClick={() => setOpenKey(openKey === o.dedup_key ? null : o.dedup_key)}
                className="flex w-full items-center justify-between gap-3 px-3 py-2.5 text-left"
              >
                <div className="flex min-w-0 items-center gap-2">
                  {severityBadge(o.severity)}
                  <div className="min-w-0">
                    <div className="truncate text-sm font-medium text-fg-1">{o.title}</div>
                    <div className="text-[11px] text-faint">
                      {o.detector} · {o.scope}
                    </div>
                  </div>
                </div>
                <div className="shrink-0 text-right">
                  {o.savings_usd != null && o.savings_usd > 0 && (
                    <div className="font-mono text-sm font-semibold text-fg">{usd(o.savings_usd)}</div>
                  )}
                  <div className="text-[11px] text-faint">{openKey === o.dedup_key ? "collapse" : "drill down"}</div>
                </div>
              </button>
              {openKey === o.dedup_key && (
                <div className="space-y-2 border-t border-line px-3 py-3 text-xs text-fg-2">
                  <p className="font-medium text-fg-1">{o.justification.claim}</p>
                  {o.nudge && <p className="text-accent">{o.nudge}</p>}
                  {o.justification.reprice && o.justification.reprice.length > 0 && (
                    <div className="overflow-hidden rounded border border-line">
                      <table className="w-full text-left text-[11px]">
                        <thead className="bg-surface uppercase tracking-wide text-faint">
                          <tr>
                            <th className="px-2 py-1 font-medium">Original</th>
                            <th className="px-2 py-1 font-medium">Replay</th>
                            <th className="px-2 py-1 text-right font-medium">Cost</th>
                          </tr>
                        </thead>
                        <tbody>
                          {o.justification.reprice.map((r, i) => (
                            <tr key={i} className="border-t border-line">
                              <td className="px-2 py-1 font-mono">{r.original_model}</td>
                              <td className="px-2 py-1 font-mono">{r.selected_model}</td>
                              <td className="px-2 py-1 text-right font-mono">{usd(r.estimated_cost_usd)}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  )}
                  {o.justification.evidence.numbers && (
                    <div className="flex flex-wrap gap-1.5">
                      {Object.entries(o.justification.evidence.numbers).map(([k, v]) => (
                        <span key={k} className="rounded border border-line bg-surface px-1.5 py-0.5 font-mono text-[10px] text-fg-2">
                          {k}={v}
                        </span>
                      ))}
                    </div>
                  )}
                  {o.justification.evidence.math && (
                    <p className="font-mono text-[10px] text-faint">{o.justification.evidence.math}</p>
                  )}
                  {o.justification.flip_conditions && (
                    <p className="text-[11px] italic text-faint">Recheck if: {o.justification.flip_conditions}</p>
                  )}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}

function AggregatesTable({ rows }: { rows: BriefingAggregate[] }) {
  return (
    <Card className="p-3">
      <div className="mb-1 px-1 text-sm font-medium text-fg-1">Pushed aggregate digest</div>
      <div className="mb-2 px-1 text-[11px] text-faint">
        Hash-only buckets nodes opted into sharing ({`[org_client.share]`} briefing_aggregates). Admin view.
      </div>
      <table className="w-full text-left text-xs">
        <thead className="text-[10px] uppercase tracking-[0.06em] text-faint">
          <tr className="border-b border-line">
            <th className="bg-surface px-2 py-2 font-medium">Tool</th>
            <th className="bg-surface px-2 py-2 font-medium">Developer</th>
            <th className="bg-surface px-2 py-2 text-right font-medium">Sessions</th>
            <th className="bg-surface px-2 py-2 text-right font-medium">Turns</th>
            <th className="bg-surface px-2 py-2 text-right font-medium">Cost</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} className="border-b border-line last:border-b-0">
              <td className="px-2 py-1.5 text-fg-1">{r.tool}</td>
              <td className="px-2 py-1.5 text-fg-2">{r.user_email || "—"}</td>
              <td className="px-2 py-1.5 text-right text-fg-2">{num(r.sessions)}</td>
              <td className="px-2 py-1.5 text-right text-fg-2">{num(r.turns)}</td>
              <td className="whitespace-nowrap px-2 py-1.5 text-right font-mono tabular-nums text-fg-1">{usd(r.cost_usd)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </Card>
  );
}
