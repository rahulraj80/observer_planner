import assert from "node:assert/strict";
import test from "node:test";

import {
  COST_HIDDEN,
  costChoice,
  costLabel,
  dollarsShown,
  fmtCost,
  fmtCostAs,
  fmtTaskUSD,
  fmtUSD,
  getCostDisplayMode,
  maskDollarAmounts,
  setCostDisplayMode,
  withoutDollars,
} from "../../../shared/lib/format.ts";
import {
  PENDING_COST_DISPLAY,
  budgetBannerSentence,
  budgetShareLine,
  costDisplayNote,
  normalizeCostDisplay,
  retryDelayMs,
  tokenSourceMix,
} from "./costDisplayModel.ts";

// The node dashboard's ONE cost formatter seam (shared/lib/format.ts) under the
// org's developer cost display (operator ruling 2026-10-02): one row per
// (mode, call) with the exact rendered string.
test("formatter seam: every call in both modes", () => {
  const rows: { mode: "usd" | "tokens"; got: () => string; want: string }[] = [
    { mode: "usd", got: () => fmtUSD(12.5), want: "$12.50" },
    { mode: "usd", got: () => fmtCost(12.5, 212_400), want: "$12.50" },
    { mode: "usd", got: () => fmtTaskUSD(0.004), want: "$0.0040" },
    { mode: "usd", got: () => fmtUSD(null), want: "—" },
    { mode: "tokens", got: () => fmtCost(12.5, 212_400), want: "212.4K tok" },
    { mode: "tokens", got: () => fmtCost(12.5, 0), want: "0 tok" },
    // No token count at the call site: hidden, never a dollar figure and
    // never a conversion of dollars into tokens.
    { mode: "tokens", got: () => fmtCost(12.5), want: COST_HIDDEN },
    { mode: "tokens", got: () => fmtUSD(12.5), want: COST_HIDDEN },
    { mode: "tokens", got: () => fmtUSD(12.5, true), want: COST_HIDDEN },
    { mode: "tokens", got: () => fmtTaskUSD(0.004), want: COST_HIDDEN },
  ];
  try {
    for (const r of rows) {
      setCostDisplayMode(r.mode);
      const got = r.got();
      assert.equal(got, r.want, `${r.mode}: ${r.got.toString()}`);
      if (r.mode === "tokens") assert.doesNotMatch(got, /\$/);
    }
  } finally {
    setCostDisplayMode("usd");
  }
  assert.equal(getCostDisplayMode(), "usd", "the default (and web2's only mode) is dollars");
});

// The affordances around a figure (labels, $ toggles, dollar-only columns)
// follow the same mode: identities in dollars, token wording / dropped
// dollar entries in tokens mode.
test("cost affordances: labels, toggles and dollar-only lists in both modes", () => {
  const opts = [
    { value: "tokens", label: "Tokens" },
    { value: "cost", label: "Cost $" },
  ];
  const isDollar = (o: { value: string }) => o.value === "cost";
  const rows: { mode: "usd" | "tokens"; got: () => unknown; want: unknown }[] = [
    { mode: "usd", got: () => dollarsShown(), want: true },
    { mode: "tokens", got: () => dollarsShown(), want: false },
    { mode: "usd", got: () => costLabel("Total $", "Total tok"), want: "Total $" },
    { mode: "tokens", got: () => costLabel("Total $", "Total tok"), want: "Total tok" },
    { mode: "usd", got: () => withoutDollars(opts, isDollar).map((o) => o.value), want: ["tokens", "cost"] },
    { mode: "tokens", got: () => withoutDollars(opts, isDollar).map((o) => o.value), want: ["tokens"] },
    { mode: "usd", got: () => costChoice("cost", ["cost"], "tokens"), want: "cost" },
    { mode: "tokens", got: () => costChoice("cost", ["cost"], "tokens"), want: "tokens" },
    { mode: "tokens", got: () => costChoice("bytes", ["usd"], "tokens"), want: "bytes" },
  ];
  try {
    for (const r of rows) {
      setCostDisplayMode(r.mode);
      assert.deepEqual(r.got(), r.want, `${r.mode}: ${r.got.toString()}`);
    }
    // withoutDollars returns a copy in both modes.
    setCostDisplayMode("usd");
    assert.notEqual(withoutDollars(opts, isDollar), opts);
  } finally {
    setCostDisplayMode("usd");
  }
});

test("maskDollarAmounts hides the amounts in prose, only in tokens mode", () => {
  const rows: [string, string][] = [
    ["$4.20 fast-tier premium across 3 turns", "— fast-tier premium across 3 turns"],
    ["$1,234 of spend isn't routed through the proxy", "— of spend isn't routed through the proxy"],
    ["save ~$0.0040 per turn (net of -$3.10 forfeit)", "save ~— per turn (net of -— forfeit)"],
    ["$12k a month", "— a month"],
    ["no money here, 40% and 3 turns", "no money here, 40% and 3 turns"],
    ["regex anchors like ^/health$ stay", "regex anchors like ^/health$ stay"],
  ];
  try {
    setCostDisplayMode("tokens");
    for (const [input, want] of rows) assert.equal(maskDollarAmounts(input), want, input);
    setCostDisplayMode("usd");
    for (const [input] of rows) assert.equal(maskDollarAmounts(input), input, input);
  } finally {
    setCostDisplayMode("usd");
  }
});

test("fmtCostAs is pure: it ignores the page's mode", () => {
  try {
    setCostDisplayMode("tokens");
    assert.equal(fmtCostAs("usd", 1.84, 212_400), "$1.84");
  } finally {
    setCostDisplayMode("usd");
  }
  assert.equal(fmtCostAs("tokens", 1.84, 212_400), "212.4K tok");
});

test("an unknown mode passed to the setter fails toward dollars", () => {
  try {
    setCostDisplayMode("percent" as unknown as "usd");
    assert.equal(getCostDisplayMode(), "usd");
  } finally {
    setCostDisplayMode("usd");
  }
});

// Sol A4-03: only an authoritative answer may put the page in dollars. A
// malformed or unknown answer is NOT authoritative (null: the gate keeps the
// token-safe pending view and retries), and an org answer this build cannot
// read takes the restrictive mode, never dollars.
test("normalizeCostDisplay: only an authoritative answer shows dollars", () => {
  const rows: [unknown, string | null, string | null][] = [
    [{ mode: "tokens", source: "org", org_name: "Acme", version: 3 }, "tokens", "org"],
    [{ mode: "usd", source: "org" }, "usd", "org"],
    [{ mode: "usd", source: "default" }, "usd", "default"],
    [{ mode: "something-new", source: "org" }, "tokens", "org"],
    [{ mode: "something-new", source: "default" }, null, null],
    [{ source: "default" }, null, null],
    [{ mode: "usd" }, null, null],
    [{ mode: "usd", source: "pending" }, null, null],
    [null, null, null],
    ["garbage", null, null],
  ];
  for (const [raw, mode, source] of rows) {
    const got = normalizeCostDisplay(raw);
    if (mode === null) {
      assert.equal(got, null, JSON.stringify(raw));
      continue;
    }
    assert.ok(got, JSON.stringify(raw));
    assert.equal(got.mode, mode, JSON.stringify(raw));
    assert.equal(got.source, source, JSON.stringify(raw));
  }
});

// Sol A4-03: while no authoritative answer has arrived (a stalled, failed or
// unreadable /api/cost-display) the page renders token-safe, never dollars,
// and keeps asking on a bounded backoff.
test("pending cost display is token-safe and retried", () => {
  assert.equal(PENDING_COST_DISPLAY.mode, "tokens");
  assert.equal(PENDING_COST_DISPLAY.source, "pending");
  assert.match(costDisplayNote(PENDING_COST_DISPLAY), /Token usage is shown until/);
  assert.doesNotMatch(costDisplayNote(PENDING_COST_DISPLAY), /\u2014/);
  const delays = [0, 1, 2, 3, 4, 5, 6, 50].map(retryDelayMs);
  assert.deepEqual(delays, [1000, 2000, 4000, 8000, 16000, 30000, 30000, 30000]);
});

test("costDisplayNote names the org, and says nothing in dollars mode", () => {
  assert.equal(costDisplayNote({ mode: "usd", source: "org", org_name: "Acme" }), "");
  assert.match(costDisplayNote({ mode: "tokens", source: "org", org_name: "Acme" }), /^Acme shows token usage instead of costs/);
  assert.match(costDisplayNote({ mode: "tokens", source: "org" }), /^Your organization shows token usage instead of costs/);
});

test("tokenSourceMix: the source mix by turns, never a cost word", () => {
  const rows: { name: string; in: { source: string; turn_count: number }[]; want: string }[] = [
    { name: "empty", in: [], want: "" },
    { name: "no turns", in: [{ source: "proxy", turn_count: 0 }], want: "" },
    { name: "one source", in: [{ source: "proxy", turn_count: 12 }], want: "100% proxy" },
    {
      name: "jsonl reads as transcript, sorted by share",
      in: [
        { source: "jsonl", turn_count: 3 },
        { source: "proxy", turn_count: 7 },
      ],
      want: "70% proxy · 30% transcript",
    },
    {
      name: "same label folds, unknown is other",
      in: [
        { source: "jsonl", turn_count: 1 },
        { source: "transcript", turn_count: 1 },
        { source: "", turn_count: 2 },
      ],
      want: "50% other · 50% transcript",
    },
  ];
  for (const r of rows) assert.equal(tokenSourceMix(r.in), r.want, r.name);
});

test("budgetShareLine: a budget as a share of its cap, no dollars", () => {
  const rows: { name: string; sc: { pct: number; budget_usd: number; forecast_usd: number }; want: string }[] = [
    { name: "under, with pace", sc: { pct: 41.6, budget_usd: 40, forecast_usd: 60 }, want: "42% of cap · day 2/31 · on pace 150%" },
    { name: "no forecast", sc: { pct: 0, budget_usd: 40, forecast_usd: 0 }, want: "0% of cap · day 2/31" },
    { name: "over", sc: { pct: 130, budget_usd: 1.5, forecast_usd: 30 }, want: "130% of cap · day 2/31 · on pace 2000%" },
  ];
  for (const r of rows) {
    const got = budgetShareLine(r.sc, 2, 31);
    assert.equal(got, r.want, r.name);
    assert.ok(!got.includes("$"), `${r.name}: no dollar sign`);
  }
});

test("budgetBannerSentence: dollar copy unchanged, token copy in shares, no em dash", () => {
  const over = { pct: 186.7, budget_usd: 1.5, forecast_usd: 43.53, mtd_usd: 2.8 };
  const under = { pct: 85.2, budget_usd: 40, forecast_usd: 30, mtd_usd: 34.08 };
  const rows: { name: string; label: string; sc: typeof over; dollars: boolean; want: string }[] = [
    {
      name: "usd, over pace",
      label: "infra",
      sc: over,
      dollars: true,
      want: "infra is at $2.80 of its $1.50 monthly budget - on pace for $43.53 by month end. Advisory only - nothing is blocked.",
    },
    {
      name: "usd, under pace",
      label: "Monthly spend",
      sc: under,
      dollars: true,
      want: "Monthly spend is at $34.08 of its $40.00 monthly budget. Advisory only - nothing is blocked.",
    },
    {
      name: "tokens, over pace",
      label: "infra",
      sc: over,
      dollars: false,
      want: "infra: 187% used of the monthly budget, on pace for 2902% by month end. Advisory only, nothing is blocked.",
    },
    {
      name: "tokens, under pace",
      label: "Monthly usage",
      sc: under,
      dollars: false,
      want: "Monthly usage: 85% used of the monthly budget. Advisory only, nothing is blocked.",
    },
  ];
  try {
    for (const r of rows) {
      setCostDisplayMode(r.dollars ? "usd" : "tokens");
      const got = budgetBannerSentence(r.label, r.sc, dollarsShown() ? (n) => fmtUSD(n) : null);
      assert.equal(got, r.want, r.name);
      assert.ok(!got.includes("—"), `${r.name}: no em dash`);
      if (!r.dollars) assert.ok(!got.includes("$"), `${r.name}: no dollar sign`);
    }
  } finally {
    setCostDisplayMode("usd");
  }
});
