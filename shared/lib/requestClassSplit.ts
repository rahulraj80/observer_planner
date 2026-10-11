// requestClassSplit - the pure half of the session drawer's request-class
// split, shared by the node drawer (web/, GET /api/session/<id>
// request_class_split) and the org drawer (web2/,
// rollup.SessionDetailResult.request_class_split). Both servers produce the
// split with the SAME internal/requestclass.Summarize, so one view model
// renders both. No JSX, no fetch. Promoted from web2/src/lib/
// orgRequestClassSplit.ts (which now re-exports this module).
//
// ABSENCE IS ABSENCE. A split the server did not send (an older server, or a
// session with no counted proxy turn) and a split whose `classified` is
// false (no turn carried the client's request-class hint) both map to null:
// the drawer renders nothing, never "100% unclassified", which would read as
// a finding about the session rather than a missing signal.

// RequestClassBucketWire mirrors requestclass.Bucket.
export interface RequestClassBucketWire {
  class: string;
  turns: number;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_creation_tokens: number;
  cost_usd: number;
}

// RequestClassSplitWire mirrors requestclass.Split.
export interface RequestClassSplitWire {
  classified: boolean;
  classes: RequestClassBucketWire[];
  unclassified: RequestClassBucketWire;
  total_turns: number;
}

// The closed vocabulary's display labels (requestclass.Values order is kept
// by the server; this only names them).
const LABELS: Record<string, string> = {
  main: "Main conversation",
  subagent: "Subagents",
  workflow: "Workflows",
  compaction: "Compaction",
  auxiliary: "Side requests",
};

export interface RequestClassRowView {
  key: string;
  label: string;
  turns: number;
  tokens: number;
  costUSD: number;
  // costShare is this row's share of the split's total cost, 0..1; null
  // when the split's total cost is 0 (a share of nothing is not 0%).
  costShare: number | null;
  unclassified: boolean;
}

export interface RequestClassSplitView {
  rows: RequestClassRowView[];
  totalTurns: number;
}

function tokensOf(b: RequestClassBucketWire): number {
  return b.input_tokens + b.output_tokens + b.cache_read_tokens + b.cache_creation_tokens;
}

// requestClassSplitView maps the wire split onto drawer rows: one row per
// reported class in the server's order, then an "Unclassified" row when some
// counted turns carried no class. null = render nothing.
export function requestClassSplitView(split: RequestClassSplitWire | undefined | null): RequestClassSplitView | null {
  if (!split || !split.classified) return null;
  const buckets = [...(split.classes ?? [])];
  const unclassified = split.unclassified && split.unclassified.turns > 0 ? split.unclassified : null;
  const totalCost = buckets.reduce((s, b) => s + b.cost_usd, 0) + (unclassified?.cost_usd ?? 0);
  const row = (b: RequestClassBucketWire, isUnclassified: boolean): RequestClassRowView => ({
    key: isUnclassified ? "unclassified" : b.class,
    label: isUnclassified ? "Unclassified" : (LABELS[b.class] ?? b.class),
    turns: b.turns,
    tokens: tokensOf(b),
    costUSD: b.cost_usd,
    costShare: totalCost > 0 ? b.cost_usd / totalCost : null,
    unclassified: isUnclassified,
  });
  const rows = buckets.map((b) => row(b, false));
  if (unclassified) rows.push(row(unclassified, true));
  return { rows, totalTurns: split.total_turns };
}
