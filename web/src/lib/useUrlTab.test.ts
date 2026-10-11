// Pins the URL rules behind the node dashboard's useUrlTab (src/lib/
// useUrlTab.ts) and web2's useUrlEnum, both thin bindings of the pure
// shared/lib/urlParams.ts helpers: page tabs PUSH (Back walks tabs), drawer
// tabs REPLACE, the default tab is the param's absence, other params survive,
// re-selecting the shown tab writes nothing (no duplicate history entry), and
// a cold deep link to any allowed value resolves to that tab (unknown values
// fall back to the default).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { enumParamWrite, pickEnum } from "../../../shared/lib/urlParams.ts";

const TABS = ["overview", "verdicts", "rules", "budgets", "prompt-guard", "mcp", "coverage"] as const;
type Tab = (typeof TABS)[number];
const DEFAULT: Tab = "overview";

function read(search: string): Tab {
  return pickEnum(new URLSearchParams(search).get("tab"), TABS, DEFAULT);
}

test("cold deep link: every allowed ?tab= value renders that tab", () => {
  for (const t of TABS) {
    const search: string = t === DEFAULT ? "" : `?tab=${t}`;
    assert.equal(read(search), t, search);
  }
});

test("cold deep link: unknown, empty or wrong-case values fall back to the default", () => {
  for (const s of ["?tab=nope", "?tab=", "?tab=Rules", "?other=rules"]) {
    assert.equal(read(s), DEFAULT, s);
  }
});

test("push mode: a page-tab change adds a history entry", () => {
  const w = enumParamWrite(new URLSearchParams("days=7"), "tab", "rules" as Tab, DEFAULT, "push");
  assert.ok(w);
  assert.equal(w.replace, false);
  assert.equal(w.params.toString(), "days=7&tab=rules");
});

test("replace mode: a drawer-tab change rewrites the current entry", () => {
  const w = enumParamWrite(new URLSearchParams("session=abc"), "dtab", "cache", "overview", "replace");
  assert.ok(w);
  assert.equal(w.replace, true);
  assert.equal(w.params.toString(), "session=abc&dtab=cache");
});

test("the default tab is written as the param's absence, other params kept", () => {
  const w = enumParamWrite(new URLSearchParams("tab=rules&days=7"), "tab", DEFAULT, DEFAULT, "push");
  assert.ok(w);
  assert.equal(w.params.toString(), "days=7");
  assert.equal(w.params.has("tab"), false);
});

test("re-selecting the shown tab writes nothing (no duplicate push)", () => {
  assert.equal(enumParamWrite(new URLSearchParams("tab=rules"), "tab", "rules" as Tab, DEFAULT, "push"), null);
  assert.equal(enumParamWrite(new URLSearchParams(""), "tab", DEFAULT, DEFAULT, "push"), null);
  assert.equal(enumParamWrite(new URLSearchParams("x=1"), "tab", DEFAULT, DEFAULT, "replace"), null);
});

test("a stale value in the URL is cleaned up when the default is chosen", () => {
  const w = enumParamWrite(new URLSearchParams("tab=gone"), "tab", DEFAULT, DEFAULT, "push");
  assert.ok(w);
  assert.equal(w.params.toString(), "");
});

test("the write never mutates the previous params", () => {
  const prev = new URLSearchParams("tab=rules");
  enumParamWrite(prev, "tab", "mcp" as Tab, DEFAULT, "push");
  assert.equal(prev.toString(), "tab=rules");
});

test("a section rail uses the same rules under ?section=", () => {
  const w = enumParamWrite(new URLSearchParams("tab=x"), "section", "identity", "general", "push");
  assert.ok(w);
  assert.equal(w.params.toString(), "tab=x&section=identity");
});

// The hook files cannot be imported by the node runner (react-router, the
// @shared alias), so their DEFAULTS are pinned as text: the node page-tab
// hook pushes on `?tab=`; web2's useUrlEnum keeps "replace" for its existing
// drawer callers (page lanes pass { history: "push" } explicitly).
test("hook defaults: web useUrlTab pushes on ?tab=, web2 useUrlEnum replaces", () => {
  const node = readFileSync(new URL("./useUrlTab.ts", import.meta.url), "utf8");
  assert.match(node, /const name = options\.param \?\? "tab";/);
  assert.match(node, /const history = options\.history \?\? "push";/);
  const org = readFileSync(new URL("../../../web2/src/lib/useUrlState.ts", import.meta.url), "utf8");
  assert.match(org, /const history = options\.history \?\? "replace";/);
  for (const src of [node, org]) {
    assert.match(src, /enumParamWrite\(params, name, v, fallback, history\)/);
    assert.match(src, /if \(write == null\) return;/);
  }
});
