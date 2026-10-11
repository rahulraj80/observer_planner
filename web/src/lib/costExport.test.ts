import assert from "node:assert/strict";
import test from "node:test";

import { COST_HIDDEN, setCostDisplayMode } from "../../../shared/lib/format.ts";
import { csvText, exportJSONText, exportPayload, isDollarKey, type CsvColumn } from "./costExport.ts";

// The same key table as internal/costtext/costtext_test.go TestIsDollarKey,
// so the Go and web exports agree on which fields are dollars.
test("isDollarKey: the shared key table", () => {
  const rows: [string, boolean][] = [
    ["cost_usd", true],
    ["ai_cost_usd", true],
    ["total_fast_cost_usd", true],
    ["usd", true],
    ["cost", true],
    ["usd_per_turn", true],
    ["spend_today_usd", true],
    ["baseline_cost_per_turn_usd", true],
    ["TotalUSD", true],
    ["costUsd", true],
    ["cost_reliability", false],
    ["cost_source", false],
    ["cost_tokens", false],
    ["input_tokens", false],
    ["usage", false],
    ["status", false],
    ["UserID", false],
  ];
  for (const [key, want] of rows) assert.equal(isDollarKey(key), want, key);
});

type Row = { model: string; input: number; usd: number; reliability: string };
const COLS: CsvColumn<Row>[] = [
  { header: "model", value: (r) => r.model },
  { header: "input_tokens", value: (r) => r.input },
  { header: "cost_usd", value: (r) => r.usd },
  { header: "cost_reliability", value: (r) => r.reliability },
];
const ROWS: Row[] = [{ model: "opus, 4", input: 1200, usd: 1.25, reliability: "accurate" }];

test("csvText follows the display: dollar columns only in dollars, units in the header", () => {
  try {
    setCostDisplayMode("usd");
    assert.equal(csvText(ROWS, COLS), 'model,input_tokens,cost_usd,cost_reliability\n"opus, 4",1200,1.25,accurate');
    setCostDisplayMode("tokens");
    const tok = csvText(ROWS, COLS);
    assert.equal(tok, 'model,input_tokens,cost_reliability\n"opus, 4",1200,accurate');
    assert.doesNotMatch(tok, /usd|1\.25/);
  } finally {
    setCostDisplayMode("usd");
  }
});

test("exportPayload / exportJSONText: dollar fields dropped and marked in the token view", () => {
  const body = { rows: [{ key: "a", cost_usd: 2, tokens: 9 }], total_cost_usd: 2, note: "saves $0.40 a day" };
  try {
    setCostDisplayMode("usd");
    assert.deepEqual(exportPayload(body), body);
    assert.equal(exportJSONText(JSON.stringify(body)), JSON.stringify(body));
    setCostDisplayMode("tokens");
    assert.deepEqual(exportPayload(body), {
      rows: [{ key: "a", tokens: 9 }],
      note: `saves ${COST_HIDDEN} a day`,
      cost_display: "tokens",
    });
    const text = exportJSONText(JSON.stringify(body));
    assert.doesNotMatch(text, /cost_usd|\$/);
    assert.equal(exportJSONText("not json $3.00"), `not json ${COST_HIDDEN}`);
  } finally {
    setCostDisplayMode("usd");
  }
});
