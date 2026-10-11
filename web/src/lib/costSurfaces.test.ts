import assert from "node:assert/strict";
import test from "node:test";

import { setCostDisplayMode } from "../../../shared/lib/format.ts";
import { arenaUsageLine, savedMilestoneCopy } from "./costSurfaces.ts";

// Sol A4-04: the Arena candidate line and the compression-savings milestone
// used to build dollar strings directly, bypassing the developer cost display.
// Under the org's token-usage view neither may carry a dollar figure.
test("arenaUsageLine follows the cost display", () => {
  try {
    setCostDisplayMode("usd");
    assert.equal(arenaUsageLine(120, 45, 0.0123), "120 in / 45 out · ≈$0.0123");
    setCostDisplayMode("tokens");
    const got = arenaUsageLine(120, 45, 0.0123);
    assert.equal(got, "120 in / 45 out");
    assert.doesNotMatch(got, /\$/);
  } finally {
    setCostDisplayMode("usd");
  }
});

test("savedMilestoneCopy is dollar-only: none in the token-usage view", () => {
  try {
    setCostDisplayMode("usd");
    const usd = savedMilestoneCopy(12.7);
    assert.ok(usd);
    assert.equal(usd.chip, "★ $12 SAVED ★");
    assert.match(usd.text, /\$12\.70 saved so far/);
    assert.match(usd.share, /\$12\.70/);
    setCostDisplayMode("tokens");
    assert.equal(savedMilestoneCopy(12.7), null);
  } finally {
    setCostDisplayMode("usd");
  }
});
