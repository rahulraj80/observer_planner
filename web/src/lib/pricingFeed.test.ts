import { test } from "node:test";
import assert from "node:assert/strict";

import { pricingFeedFromConfig } from "./pricingFeed.ts";

// /api/config serializes the Go struct, so the keys are Go field names
// (Pricing.Feed.PollIntervalHours), not TOML keys. The row used to read the
// TOML spelling and so never rendered.
test("pricingFeedFromConfig reads the Go field names /api/config returns", () => {
  const rows: [Record<string, unknown> | undefined, ReturnType<typeof pricingFeedFromConfig>][] = [
    [undefined, null],
    [{}, null],
    [{ Pricing: {} }, null],
    [
      { Pricing: { Feed: { Enabled: false, Auto: false, URL: "https://x/feed", PollIntervalHours: 24 } } },
      { enabled: false, auto: false, url: "https://x/feed", pollIntervalHours: 24 },
    ],
    [
      { Pricing: { Feed: { Enabled: true, Auto: true } } },
      { enabled: true, auto: true, url: "", pollIntervalHours: 24 },
    ],
  ];
  for (const [cfg, want] of rows) assert.deepEqual(pricingFeedFromConfig(cfg), want);
});
