// Pins the Requests page's entry points, including the guard deny line's
// /requests?rule=&session= deep link (request-grants G4).
import { test } from "node:test";
import assert from "node:assert/strict";
import { budgetRequestLink, requestEntryFrom, withoutEntryParams } from "./requestEntry.ts";

const q = (s: string) => new URLSearchParams(s);

test("the deny line's rule and session prefill a guard exemption", () => {
  assert.deepEqual(requestEntryFrom(q("rule=R-171&session=s-1")), {
    kind: "prefill",
    query: { rule: "R-171", session: "s-1" },
    fallback: { request_type: "guard_exemption", message: "", payload: { rule_id: "R-171", breadth: "rule" } },
  });
});

test("a rule without a session prefills rule-wide", () => {
  const e = requestEntryFrom(q("rule=R-152"));
  assert.ok(e && e.kind === "prefill");
  assert.deepEqual(e.query, { rule: "R-152" });
});

test("the Security page's event id wins over a rule", () => {
  const e = requestEntryFrom(q("event=42&rule=R-171"));
  assert.ok(e && e.kind === "prefill");
  assert.deepEqual(e.query, { event_id: "42" });
});

test("a malformed rule, session or event is ignored, never sent", () => {
  assert.equal(requestEntryFrom(q("rule=" + encodeURIComponent("R-1\nx"))), null);
  assert.equal(requestEntryFrom(q("rule=" + "R".repeat(65))), null);
  assert.equal(requestEntryFrom(q("event=-3")), null);
  const e = requestEntryFrom(q("rule=R-1&session=" + encodeURIComponent("a\u0007b")));
  assert.ok(e && e.kind === "prefill");
  assert.deepEqual(e.query, { rule: "R-1" });
});

test("a type opens an empty draft; nothing opens nothing", () => {
  assert.deepEqual(requestEntryFrom(q("type=budget_increase")), {
    kind: "type",
    draft: { request_type: "budget_increase", message: "", payload: {} },
  });
  assert.equal(requestEntryFrom(q("tab=mine")), null);
});

test("consumed entry params are cleared, others kept", () => {
  assert.equal(withoutEntryParams(q("rule=R-1&session=s&tab=mine&event=3&type=x")).toString(), "tab=mine");
});

// T-522: the budget banner, its "+N more" list and the guard's budget deny
// line open the same form through one stable link.
test("new=budget opens a budget increase, on the named budget when given", () => {
  assert.deepEqual(requestEntryFrom(q("new=budget")), {
    kind: "type",
    draft: { request_type: "budget_increase", message: "", payload: {} },
  });
  assert.deepEqual(requestEntryFrom(q("new=budget&budget=b-42")), {
    kind: "type",
    draft: { request_type: "budget_increase", message: "", payload: { budget_id: "b-42" } },
  });
  assert.deepEqual(requestEntryFrom(q("type=budget_increase&budget=b-7")), {
    kind: "type",
    draft: { request_type: "budget_increase", message: "", payload: { budget_id: "b-7" } },
  });
  // A malformed id is dropped, never matched.
  const e = requestEntryFrom(q("new=budget&budget=" + encodeURIComponent("b\u0000x")));
  assert.ok(e && e.kind === "type");
  assert.deepEqual(e.draft.payload, {});
  // An unknown new= kind opens nothing.
  assert.equal(requestEntryFrom(q("new=something")), null);
  // A budget id rides only on a budget request.
  const g = requestEntryFrom(q("type=tool_allow&budget=b-1"));
  assert.ok(g && g.kind === "type");
  assert.deepEqual(g.draft.payload, {});
});

test("budgetRequestLink is the stable deep link", () => {
  assert.equal(budgetRequestLink(), "/requests?new=budget");
  assert.equal(budgetRequestLink("b-42"), "/requests?new=budget&budget=b-42");
  assert.equal(budgetRequestLink("a b/c"), "/requests?new=budget&budget=a%20b%2Fc");
  assert.equal(budgetRequestLink("x\ny"), "/requests?new=budget");
  const e = requestEntryFrom(new URL("http://n" + budgetRequestLink("a b/c")).searchParams);
  assert.ok(e && e.kind === "type");
  assert.deepEqual(e.draft.payload, { budget_id: "a b/c" });
});

test("the budget link's params are consumed too", () => {
  assert.equal(withoutEntryParams(q("new=budget&budget=b-1&tab=mine")).toString(), "tab=mine");
});

// T-522: the banner's top notice and every "+N more" entry ask through the
// one link, for the notice's own budget.
test("the budget warning banner asks through budgetRequestLink", async () => {
  const { readFileSync } = await import("node:fs");
  const src = readFileSync(new URL("../components/BudgetWarningBanner.tsx", import.meta.url), "utf8");
  assert.match(src, /to=\{budgetRequestLink\(view\.top\.budget_id\)\}/);
  assert.match(src, /to=\{budgetRequestLink\(n\.budget_id\)\}/);
  assert.match(src, /const ASK_LABEL = "Ask for a budget increase";/);
});

// T-529: the guard's budget deny line (internal/guard/hookbudget.go
// BudgetIncreaseRequestPath) is the stable budget link, and this page opens
// it as a budget increase on the budget that ran out. The Go side pins the
// same constant against ENTRY_PARAMS / NEW_KINDS (TestT529DenyLinkOpensTheBudgetRequest).
test("the guard's budget deny link is budgetRequestLink and opens a budget increase", async () => {
  const { readFileSync } = await import("node:fs");
  const src = readFileSync(new URL("../../../internal/guard/hookbudget.go", import.meta.url), "utf8");
  const m = src.match(/const BudgetIncreaseRequestPath = "([^"]+)"/);
  assert.ok(m, "BudgetIncreaseRequestPath not found in hookbudget.go");
  const path = m[1];
  assert.equal(path, budgetRequestLink());
  const u = new URL("http://n" + path);
  assert.equal(u.pathname, "/requests");
  assert.deepEqual(requestEntryFrom(u.searchParams), {
    kind: "type",
    draft: { request_type: "budget_increase", message: "", payload: {} },
  });
  assert.equal(withoutEntryParams(u.searchParams).toString(), "");
});
