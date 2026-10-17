import assert from "node:assert/strict";
import test from "node:test";

import { NODE_TAB_ROUTES, nodeTabCrumb, nodeTabJumps } from "./pageTabRoutes.ts";
import { reportToCostSearch } from "./nodePageTabs.ts";

// nav.ts imports lucide-react, which a plain node:test can load; the pin is
// that every tabbed page is a real sidebar entry with the same path.
import { NAV_ITEMS } from "./nav.ts";

test("NODE_TAB_ROUTES: every tabbed page is a sidebar entry at the same path", () => {
  for (const r of NODE_TAB_ROUTES) {
    const item = NAV_ITEMS.find((n) => n.id === r.navId);
    assert.ok(item, `${r.navId} is not a nav item`);
    assert.equal(item!.path, r.path);
  }
});

test("nodeTabJumps: the tabs the lanes asked for are jump targets, defaults are not", () => {
  const to = new Set(nodeTabJumps().map((j) => j.to));
  for (const want of [
    "/security?tab=prompt-guard",
    "/security?tab=coverage",
    "/cost?tab=statement",
    "/cost?tab=by-model",
    "/compression?tab=experiments",
    "/cache?tab=events",
    "/routing?tab=shadow",
    "/policies?tab=guardrails",
    "/terminals?tab=settings",
    "/terminals?tab=arena",
    "/remote?tab=tailscale",
  ]) {
    assert.ok(to.has(want), `missing palette jump ${want}`);
  }
  for (const r of NODE_TAB_ROUTES) {
    assert.ok(!to.has(`${r.path}?tab=${r.tabs.ids[0]}`), `${r.path} default tab listed as a jump`);
  }
});

test("nodeTabJumps: the retired Report page is found by its old name", () => {
  const j = nodeTabJumps().find((x) => x.aliases.includes("Monthly statement"));
  assert.equal(j?.to, "/cost?tab=statement");
});

const CRUMBS: Array<{ path: string; search: string; want: string | null }> = [
  { path: "/security", search: "?tab=prompt-guard", want: "Prompt guard" },
  { path: "/security", search: "?tab=overview", want: null },
  { path: "/security", search: "", want: null },
  { path: "/security", search: "?tab=bogus", want: null },
  { path: "/cost", search: "?tab=statement&month=2026-09", want: "Statement" },
  { path: "/terminals", search: "?tab=arena", want: "Arena" },
  // A drawer's ?dtab= is not a page tab.
  { path: "/sessions", search: "?session=s1&dtab=messages", want: null },
  { path: "/live", search: "?tab=cache", want: null },
];
for (const c of CRUMBS) {
  test(`nodeTabCrumb: ${c.path}${c.search}`, () => {
    assert.equal(nodeTabCrumb(c.path, c.search), c.want);
  });
}

test("the /report redirect lands on a real Cost tab and keeps every param", () => {
  const search = reportToCostSearch("?month=2026-08&project=%2Fhome%2Fa");
  assert.equal(nodeTabCrumb("/cost", search), "Statement");
  const p = new URLSearchParams(search);
  assert.equal(p.get("month"), "2026-08");
  assert.equal(p.get("project"), "/home/a");
});
