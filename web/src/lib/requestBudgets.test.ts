// Pins the budget picker on a budget-increase request (T-523, D-334): never a
// typed id, a default on the budget that ran out, and an amount that follows
// the picked budget's unit under the org's cost display.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  NO_BUDGET,
  budgetAmountFields,
  budgetDeltaHelp,
  budgetOptionLabel,
  budgetOptions,
  budgetDurationOptions,
  budgetPickNote,
  budgetSuggestion,
  capWords,
  defaultBudgetId,
  scopeWords,
  type RequestableBudget,
} from "./requestBudgets.ts";

test("scopeWords: whose budget it is", () => {
  const rows: Array<[string, string]> = [
    ["org", "Organization budget"],
    ["member:u1", "Your budget"],
    ["team:eng", "Team budget (eng)"],
    ["tool:codex", "Organization budget for tool codex"],
    ["model:gpt-5", "Organization budget for model gpt-5"],
    ["", "Budget"],
  ];
  for (const [scope, want] of rows) assert.equal(scopeWords(scope), want);
});

test("capWords: tokens first, then USD, never blank", () => {
  assert.equal(capWords({ budget_id: "b", scope: "org", period: "calendar_month", cap_tokens: 1_000_000 }, true), "1.0M tokens");
  assert.equal(capWords({ budget_id: "b", scope: "org", period: "calendar_month", cap_tokens: 200_000 }, true), "200k tokens");
  assert.equal(capWords({ budget_id: "b", scope: "org", period: "calendar_month", cap_usd: 50 }, true), "50.00 USD");
  assert.equal(capWords({ budget_id: "b", scope: "org", period: "calendar_month" }, true), "no cap");
});

// M4-03 / D-013 / D-179: a developer on a token-usage org display policy (or
// one still pending) never sees a dollar amount: a token cap still reads in
// tokens, a dollar-only cap reads as "a spend cap" with no figure.
test("capWords in token-usage mode: no dollar amount, ever", () => {
  const rows: Array<[RequestableBudget, string]> = [
    [{ budget_id: "b", scope: "org", period: "calendar_month", cap_tokens: 1_000_000 }, "1.0M tokens"],
    [{ budget_id: "b", scope: "org", period: "calendar_month", cap_tokens: 200_000, cap_usd: 50 }, "200k tokens"],
    [{ budget_id: "b", scope: "org", period: "calendar_month", cap_usd: 50 }, "a spend cap"],
    [{ budget_id: "b", scope: "org", period: "calendar_month" }, "no cap"],
  ];
  for (const [b, want] of rows) {
    const got = capWords(b, false);
    assert.equal(got, want);
    assert.doesNotMatch(got, /USD|\$|50/);
  }
});

test("budgetOptionLabel names scope, period, subject, cap, state and sharing", () => {
  assert.equal(
    budgetOptionLabel({ budget_id: "b", scope: "member:u1", period: "calendar_month", cap_tokens: 1_000_000 }, true),
    "Your budget, monthly, 1.0M tokens",
  );
  assert.equal(
    budgetOptionLabel(
      { budget_id: "b", scope: "team:eng", period: "calendar_day", subject: { kind: "tool", id: "codex" }, cap_tokens: 200_000, grantable: false },
      true,
    ),
    "Team budget (eng), daily, for tool codex, 200k tokens (shared)",
  );
  assert.equal(
    budgetOptionLabel({ budget_id: "b", scope: "member:u1", period: "calendar_month", cap_usd: 75, level: "exceeded" }, false),
    "Your budget, monthly, a spend cap, exceeded",
  );
  assert.equal(
    budgetOptionLabel({ budget_id: "b", scope: "member:u1", period: "calendar_day", cap_tokens: 5000, level: "warning", ratio: 0.9 }, true),
    "Your budget, daily, 5k tokens, 90% used",
  );
  // The prefilled pick carries the current use (node 1, 2026-10-06), and a
  // reading under the cap is never called exceeded.
  assert.equal(
    budgetOptionLabel({ budget_id: "b", scope: "member:u1", period: "calendar_day", cap_tokens: 5000, level: "exceeded", ratio: 1.2 }, true),
    "Your budget, daily, 5k tokens, exceeded (120% used)",
  );
  assert.equal(
    budgetOptionLabel({ budget_id: "b", scope: "team:eng", period: "calendar_month", cap_tokens: 10_000_000, level: "exceeded", ratio: 0.110238 }, true),
    "Team budget (eng), monthly, 10.0M tokens, 11% used",
  );
  assert.equal(
    budgetOptionLabel({ budget_id: "b", scope: "team:eng", period: "calendar_month", cap_tokens: 10_000_000, level: "ok", ratio: 0.110238 }, true),
    "Team budget (eng), monthly, 10.0M tokens, 11% used",
  );
});

test("budgetOptions lists the budgets, then the organization's choice; never a typed id", () => {
  const opts = budgetOptions([{ budget_id: "b-u1", scope: "member:u1", period: "calendar_month", cap_tokens: 5000 }], true);
  assert.equal(opts[0]!.value, "b-u1");
  assert.deepEqual(opts[1], { value: NO_BUDGET, label: "Not sure: let the organization pick" });
  assert.equal(budgetOptions([], true).length, 1);
  for (const o of opts) assert.doesNotMatch(o.label, /b-u1/);
});

test("budgetOptions in token-usage mode carries no dollar figure", () => {
  const opts = budgetOptions(
    [
      { budget_id: "b1", scope: "org", period: "calendar_month", cap_usd: 1234.5, grantable: false },
      { budget_id: "b2", scope: "team:eng", period: "calendar_day", cap_tokens: 200_000, grantable: false },
    ],
    false,
  );
  for (const o of opts) assert.doesNotMatch(o.label, /USD|\$|1234/);
  assert.equal(opts[0]!.label, "Organization budget, monthly, a spend cap (shared)");
});

test("defaultBudgetId: the asked one, else the one that ran out, else your only one", () => {
  const own: RequestableBudget = { budget_id: "own", scope: "member:u1", period: "calendar_day", cap_tokens: 5, grantable: true };
  const ownMonth: RequestableBudget = { budget_id: "own-m", scope: "member:u1", period: "calendar_month", cap_tokens: 50, grantable: true };
  const team: RequestableBudget = { budget_id: "team", scope: "team:eng", period: "calendar_day", cap_usd: 5, grantable: false };
  const rows: Array<[string, RequestableBudget[], string | undefined, string]> = [
    ["asked and known", [own, ownMonth], "own-m", "own-m"],
    ["asked but unknown falls through", [own], "nope", "own"],
    ["exceeded beats warning", [{ ...own, level: "warning", ratio: 0.9 }, { ...ownMonth, level: "exceeded", ratio: 1.1 }], undefined, "own-m"],
    ["your own exceeded before a shared one", [{ ...team, level: "exceeded", ratio: 3 }, { ...own, level: "exceeded", ratio: 1.2 }], undefined, "own"],
    ["a shared pool that ran out is still the default", [{ ...team, level: "exceeded", ratio: 3 }, own, ownMonth], undefined, "team"],
    ["furthest over among equals", [{ ...own, level: "exceeded", ratio: 1.2 }, { ...ownMonth, level: "exceeded", ratio: 1.6 }], undefined, "own-m"],
    ["nothing ran out, one of yours", [own, team], undefined, "own"],
    ["nothing ran out, several of yours", [own, ownMonth], undefined, NO_BUDGET],
    ["nothing known", [], undefined, NO_BUDGET],
  ];
  for (const [name, budgets, asked, want] of rows) assert.equal(defaultBudgetId(budgets, asked), want, name);
});

test("budgetAmountFields: the picked budget's units, never dollars in token mode", () => {
  const tok: RequestableBudget = { budget_id: "t", scope: "member:u1", period: "calendar_day", cap_tokens: 5 };
  const usd: RequestableBudget = { budget_id: "u", scope: "member:u1", period: "calendar_day", cap_usd: 5 };
  const both: RequestableBudget = { budget_id: "b", scope: "member:u1", period: "calendar_day", cap_tokens: 5, cap_usd: 5 };
  const rows: Array<[RequestableBudget | undefined, boolean, { tokens: boolean; usd: boolean }]> = [
    [tok, true, { tokens: true, usd: false }],
    [usd, true, { tokens: false, usd: true }],
    [usd, false, { tokens: false, usd: false }],
    [both, false, { tokens: true, usd: false }],
    [both, true, { tokens: true, usd: true }],
    [undefined, true, { tokens: true, usd: true }],
    [undefined, false, { tokens: true, usd: false }],
  ];
  for (const [picked, dollars, want] of rows) assert.deepEqual(budgetAmountFields(picked, dollars), want);
});

test("budgetDeltaHelp stands in for a missing amount field, in words", () => {
  const spend: RequestableBudget = { budget_id: "s", scope: "member:u1", period: "calendar_month", cap_usd: 50 };
  const tokens: RequestableBudget = { budget_id: "t", scope: "member:u1", period: "calendar_month", cap_tokens: 5000 };
  assert.equal(budgetDeltaHelp(true, spend), undefined);
  assert.equal(budgetDeltaHelp(true, undefined), undefined);
  assert.equal(
    budgetDeltaHelp(false, spend),
    "This budget is a spend cap. Say in your message how much more you need; the approver sets the amount.",
  );
  assert.equal(budgetDeltaHelp(false, tokens), undefined);
  assert.equal(budgetDeltaHelp(false, undefined), "Ask in tokens. The approver can also raise a spend cap and sets the amount.");
});

test("budgetPickNote: a shared pool and an empty list are explained", () => {
  const team: RequestableBudget = { budget_id: "team", scope: "team:eng", period: "calendar_day", cap_usd: 5, grantable: false };
  assert.match(budgetPickNote(team, 2) ?? "", /shared with others/);
  assert.match(budgetPickNote(undefined, 0) ?? "", /organization picks/);
  assert.equal(budgetPickNote({ ...team, grantable: true }, 2), undefined);
});

// The dialog itself: the dollar field and the dollar payload exist only when
// dollars are shown, the amount follows the picked budget, and the budget is
// picked, never typed.
test("Requests budget dialog: a picker, units from the budget, dollars gated", () => {
  const src = readFileSync(new URL("../pages/Requests.tsx", import.meta.url), "utf8");
  assert.match(src, /\{fields\.usd && \(\s*<Input\s+label="Additional dollars"/);
  assert.match(src, /budgetOptions\(budgets, dollars\)/);
  assert.match(src, /if \(!dollars\) delete p\.delta_usd;/);
  assert.match(src, /if \(!fields\.tokens\) delete p\.delta_tokens;/);
  assert.doesNotMatch(src, /label="Budget id"|placeholder="budget id"/i);
});

// T-522: opened from the banner, the dialog is prefilled with the period end
// as the window and the amount over as the increase, in displayable units.
test("budgetSuggestion: period end and amount over, in the units shown", () => {
  const now = 1_000_000 * 1e9;
  const b: RequestableBudget = {
    budget_id: "b", scope: "member:u1", period: "calendar_day", cap_tokens: 1000, cap_usd: 5,
    level: "exceeded", over_tokens: 201, over_usd: 0.52, period_end_ns: now + 3600 * 1e9,
  };
  assert.deepEqual(budgetSuggestion(b, true, now), { duration_s: 3600, delta_tokens: 201, delta_usd: 0.52 });
  assert.deepEqual(budgetSuggestion(b, false, now), { duration_s: 3600, delta_tokens: 201 });
  assert.deepEqual(budgetSuggestion({ ...b, period_end_ns: now - 1 }, true, now), { delta_tokens: 201, delta_usd: 0.52 });
  assert.deepEqual(budgetSuggestion({ ...b, over_tokens: 0, over_usd: 0, level: "warning" }, true, now), { duration_s: 3600 });
  assert.deepEqual(budgetSuggestion(undefined, true, now), {});
});

test("budgetDurationOptions end at the budget period", () => {
  const presets = [
    { value: "3600", label: "1 hour" },
    { value: "86400", label: "24 hours" },
    { value: "604800", label: "7 days" },
  ];
  const human = (s: number) => `${s}s`;
  assert.deepEqual(budgetDurationOptions(presets, 90000, human), [
    { value: "3600", label: "1 hour" },
    { value: "86400", label: "24 hours" },
    { value: "90000", label: "Until this budget period ends (90000s)" },
  ]);
  assert.deepEqual(budgetDurationOptions(presets, undefined, human), presets);
});
