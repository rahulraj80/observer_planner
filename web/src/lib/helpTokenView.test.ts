import { test } from "node:test";
import assert from "node:assert/strict";
import { setCostDisplayMode } from "../../../shared/lib/format.ts";
import { HELP_REGISTRY, getHelp, helpRegistry } from "./help.ts";
import { HELP_TOKEN_VIEW } from "./helpTokenView.ts";

const TEXT = ["title", "oneLiner", "detail", "formula", "example"] as const;

test("usd mode serves the registry verbatim", () => {
  setCostDisplayMode("usd");
  assert.equal(helpRegistry(), HELP_REGISTRY);
});

test("every token-view override names a real entry", () => {
  const ids = new Set(HELP_REGISTRY.map((e) => e.id));
  for (const id of Object.keys(HELP_TOKEN_VIEW)) assert.ok(ids.has(id), `stale override ${id}`);
});

test("token-usage mode: overridden entries carry no dollar wording", () => {
  try {
    setCostDisplayMode("tokens");
    for (const id of Object.keys(HELP_TOKEN_VIEW)) {
      const e = getHelp(id);
      assert.ok(e, id);
      for (const k of TEXT) {
        const v: string = e[k] ?? "";
        assert.ok(!v.includes("$"), `${id}.${k} has a $: ${v}`);
        assert.ok(!/\bUSD\b/.test(v), `${id}.${k} says USD`);
      }
    }
  } finally {
    setCostDisplayMode("usd");
  }
});

test("token-usage mode: no dollar figure anywhere in the registry", () => {
  try {
    setCostDisplayMode("tokens");
    const all = helpRegistry();
    assert.equal(all.length, HELP_REGISTRY.length);
    for (const e of all) {
      for (const k of TEXT) {
        const v: string = e[k] ?? "";
        assert.ok(!/\$\s?\d/.test(v), `${e.id}.${k} shows a dollar figure: ${v}`);
      }
    }
  } finally {
    setCostDisplayMode("usd");
  }
});
