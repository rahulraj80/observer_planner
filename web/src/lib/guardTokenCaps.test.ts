import assert from "node:assert/strict";
import test from "node:test";

import { TOKEN_CAP_WINDOWS, capUsePct, tokenCapDraft } from "./guardTokenCaps.ts";

test("the token cap rows cover the four [guard.budget] token keys once each", () => {
  assert.deepEqual(
    TOKEN_CAP_WINDOWS.map((w) => w.configKey),
    ["SessionTokens", "DailyTokens", "WeeklyTokens", "MonthlyTokens"],
  );
  // The per-session cap has no node-wide usage figure; every window does.
  assert.equal(TOKEN_CAP_WINDOWS[0].usageField, undefined);
  for (const w of TOKEN_CAP_WINDOWS.slice(1)) assert.ok(w.usageField, w.id);
});

test("tokenCapDraft: one row per input shape", () => {
  const rows: [string, number | null][] = [
    ["2000000", 2_000_000],
    ["2,000,000", 2_000_000],
    ["2_000_000", 2_000_000],
    ["2M", 2_000_000],
    ["1.5m", 1_500_000],
    ["750k", 750_000],
    ["0", 0],
    [" 42 ", 42],
    ["", null],
    ["-5", null],
    ["1.5", null],
    ["ten", null],
    ["$5", null],
    ["5000B", null],
  ];
  for (const [input, want] of rows) {
    const got = tokenCapDraft(input);
    if (want === null) assert.equal(got.ok, false, input);
    else assert.deepEqual(got, { ok: true, value: want }, input);
  }
});

test("capUsePct: a share only when the cap is on", () => {
  assert.equal(capUsePct(500, 1000), 50);
  assert.equal(capUsePct(500, 0), null);
  assert.equal(capUsePct(undefined, 1000), null);
});
