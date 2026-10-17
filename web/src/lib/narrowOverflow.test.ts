import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// T-339: at 375 px, hovering Settings' config path opened a 420 px tooltip
// that floating-ui could not fit inside its 8 px shift padding, so the page
// scrolled 8 px sideways (scrollWidth 383, measured live). The shared
// Tooltip now caps its bubble at the viewport less that padding.
test("the shared Tooltip bubble is capped to the viewport", () => {
  const src = readFileSync(new URL("../../../shared/primitives/Tooltip.tsx", import.meta.url), "utf8");
  assert.match(src, /maxWidth: `min\(\$\{maxWidth\}px, calc\(100vw - 16px\)\)`/);
  // The cap must match shift()'s padding on both sides.
  assert.match(src, /shift\(\{ padding: 8 \}\)/);
});
