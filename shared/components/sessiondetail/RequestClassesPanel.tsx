import { Card, Meter } from "../../primitives";
import { fmtInt } from "../../lib/format";
import { requestClassSplitView, type RequestClassSplitWire } from "../../lib/requestClassSplit";
import { defaultRenderCost, type RenderCost } from "./cost";

// RequestClassesPanel - "Spend by request class": the session's counted proxy
// turns by the client-declared request class (Claude Code's
// x-claude-code-request-class hint: main / subagent / workflow / compaction /
// auxiliary). Both drawers render it over the one requestclass.Split wire
// shape: the node from GET /api/session/<id>, the org from its session
// detail. Renders NOTHING when the split is absent or no turn carried a
// class: an absent signal is not a finding. Every dollar figure goes through
// the renderCost slot (the node's default honours the developer cost display;
// the org passes its <Money>).
export function RequestClassesPanel({
  split,
  renderCost = defaultRenderCost,
}: {
  split: RequestClassSplitWire | undefined | null;
  renderCost?: RenderCost;
}) {
  const view = requestClassSplitView(split);
  if (!view) return null;
  return (
    <Card
      title="Spend by request class"
      sub={`${fmtInt(view.totalTurns)} counted proxy turn${view.totalTurns === 1 ? "" : "s"}, as the client labelled them`}
    >
      <ul className="space-y-2">
        {view.rows.map((r) => (
          <li key={r.key} className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1">
            <span className={r.unclassified ? "text-[12px] text-fg-3" : "text-[12px] text-fg-1"}>{r.label}</span>
            <span className="text-right font-mono text-[12px] tabular-nums text-fg-1">
              {renderCost(r.costUSD, { tokens: r.tokens, kind: "api" })}
            </span>
            <Meter
              ratio={r.costShare ?? 0}
              tone={r.unclassified ? "neutral" : "accent"}
              label={r.label + ": " + (r.costShare === null ? "share unknown" : Math.round(r.costShare * 100) + "% of cost")}
            />
            <span className="text-right text-[11px] text-fg-3">
              {fmtInt(r.turns)} turn{r.turns === 1 ? "" : "s"} · {fmtInt(r.tokens)} tokens
            </span>
          </li>
        ))}
      </ul>
    </Card>
  );
}
