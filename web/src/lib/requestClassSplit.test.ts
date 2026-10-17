import assert from "node:assert/strict";
import test from "node:test";

import { requestClassSplitView } from "./requestClassSplit.ts";

// The node drawer renders the request-class split the daemon ships on GET
// /api/session/<id> (it shipped it and never rendered it before the
// node-parity lane). Same view model as the org drawer, through the node's
// import site.

const bucket = (cls: string, turns: number, cost: number) => ({
  class: cls,
  turns,
  input_tokens: turns,
  output_tokens: turns,
  cache_read_tokens: 0,
  cache_creation_tokens: 0,
  cost_usd: cost,
});

test("the node drawer renders a classified split, server order, unclassified last", () => {
  const v = requestClassSplitView({
    classified: true,
    classes: [bucket("main", 3, 0.6), bucket("compaction", 1, 0.2)],
    unclassified: bucket("", 1, 0.2),
    total_turns: 5,
  });
  assert.ok(v);
  assert.deepEqual(v.rows.map((r) => r.label), ["Main conversation", "Compaction", "Unclassified"]);
  assert.equal(v.totalTurns, 5);
});

test("an older daemon (no split) or an unclassified-only session renders nothing", () => {
  assert.equal(requestClassSplitView(undefined), null);
  assert.equal(
    requestClassSplitView({ classified: false, classes: [], unclassified: bucket("", 2, 0.1), total_turns: 2 }),
    null,
  );
});
