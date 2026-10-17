import { LineageBanner as SharedLineageBanner } from "@shared/components/sessiondetail";
import type { LineageChild } from "@shared/lib/sessionLineage";
import type { SessionDetail } from "@/lib/types";
import { fmtCompact, fmtUSD } from "@/lib/format";

// LineageBanner: codex fork / subagent lineage above the node drawer's tab
// strip. The banner itself lives in shared/ (shared/components/sessiondetail/
// LineageBanner.tsx, promoted for the org drawer, T-449); this file only
// resolves the node's session row into the shared LineageInput, so its
// importers are unchanged.
export function LineageBanner({
  d,
  onOpenSession,
}: {
  d: SessionDetail;
  onOpenSession?: (id: string) => void;
}) {
  const isSubagent = d.thread_source === "subagent";
  const parentId = d.forked_from_id || d.parent_thread_id || "";
  const children: LineageChild[] = (d.children ?? []).map((c) => {
    const toks = (c.input_tokens ?? 0) + (c.output_tokens ?? 0);
    const meta =
      toks > 0
        ? `${fmtCompact(toks)} tok${(c.cost_usd ?? 0) > 0 ? ` · ${fmtUSD(c.cost_usd)}` : ""}`
        : (c.action_count ?? 0) > 0
          ? `${c.action_count} action${(c.action_count ?? 0) === 1 ? "" : "s"}`
          : undefined;
    return { id: c.id, kind: c.thread_source === "subagent" ? "subagent" : "fork", meta };
  });
  return (
    <SharedLineageBanner
      parentId={parentId}
      parentKind={isSubagent ? "subagent" : "fork"}
      parentOpenable={Boolean(d.parent_in_db)}
      parentClosedReason={!d.parent_in_db ? "Parent session not in this database" : undefined}
      children={children}
      sidechainCount={d.sidechain_action_count ?? 0}
      onOpenSession={onOpenSession}
    />
  );
}
