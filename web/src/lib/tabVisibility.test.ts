import { test } from "node:test";
import assert from "node:assert/strict";
import { hiddenTabIds, tabId, tabOfPath, withVisibleTabs, withoutHiddenTabPaths } from "./tabVisibility.ts";
import { CACHE_TABS, SECURITY_TABS } from "./nodePageTabs.ts";
import { NODE_TAB_ROUTES } from "./pageTabRoutes.ts";

test("hiddenTabIds reads the resolved posture and is empty without it", () => {
  assert.deepEqual(hiddenTabIds(null), []);
  assert.deepEqual(hiddenTabIds({}), []);
  assert.deepEqual(hiddenTabIds({ visibility: {} }), []);
  assert.deepEqual(hiddenTabIds({ visibility: { hidden_tabs: ["security/mcp"] } }), ["security/mcp"]);
});

test("withVisibleTabs drops only this page's hidden tabs and keeps strip order", () => {
  const out = withVisibleTabs("security", SECURITY_TABS, ["security/prompt-guard", "cache/events"]);
  assert.deepEqual(out.ids, ["overview", "verdicts", "rules", "budgets", "mcp", "coverage"]);
  assert.equal(out.labels.mcp, "MCP");
});

test("withVisibleTabs is the identity when nothing on the page is hidden", () => {
  assert.equal(withVisibleTabs("security", SECURITY_TABS, []), SECURITY_TABS);
  assert.equal(withVisibleTabs("security", SECURITY_TABS, ["cache/events"]), SECURITY_TABS);
});

test("hiding the default tab makes the next tab the default", () => {
  const out = withVisibleTabs("cache", CACHE_TABS, ["cache/overview"]);
  assert.deepEqual(out.ids, ["events"]);
});

test("every tab hidden keeps the full table (the daemon hides the page itself)", () => {
  const out = withVisibleTabs("cache", CACHE_TABS, ["cache/overview", "cache/events"]);
  assert.equal(out, CACHE_TABS);
});

test("tabOfPath maps an address onto its tab, the default tab when none is named", () => {
  assert.equal(tabOfPath("/security?tab=prompt-guard", NODE_TAB_ROUTES), "security/prompt-guard");
  assert.equal(tabOfPath("/security", NODE_TAB_ROUTES), "security/overview");
  assert.equal(tabOfPath("/security?tab=nope", NODE_TAB_ROUTES), "security/overview");
  assert.equal(tabOfPath("/sessions", NODE_TAB_ROUTES), null);
  assert.equal(tabId("cache", "events"), "cache/events");
});

test("withoutHiddenTabPaths drops palette jumps to a hidden tab only", () => {
  const items = [{ to: "/security?tab=prompt-guard" }, { to: "/security?tab=mcp" }, { to: "/sessions" }, {}];
  const out = withoutHiddenTabPaths(items, (i) => (i as { to?: string }).to, ["security/prompt-guard"], NODE_TAB_ROUTES);
  assert.deepEqual(out, [{ to: "/security?tab=mcp" }, { to: "/sessions" }, {}]);
  assert.equal(withoutHiddenTabPaths(items, (i) => (i as { to?: string }).to, [], NODE_TAB_ROUTES).length, 4);
});
