// T-465: one command palette shell (shared/components/palette/
// CommandPalette.tsx + the pure shared/lib/paletteModel.ts); each app only
// supplies its sources and row bodies.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { paletteClamp, paletteFlatKeys, paletteMove, type PaletteKey } from "../../../shared/lib/paletteModel.ts";

const repo = fileURLToPath(new URL("../../../", import.meta.url));

// [cursor, key, row count, expected cursor]
const MOVES: Array<[number, PaletteKey, number, number]> = [
  [0, "ArrowDown", 3, 1],
  [2, "ArrowDown", 3, 2], // stops at the end (no wrap)
  [1, "ArrowUp", 3, 0],
  [0, "ArrowUp", 3, 0], // stops at the top
  [1, "Home", 3, 0],
  [0, "End", 3, 2],
  [0, "ArrowDown", 0, 0], // empty list
  [5, "End", 0, 0],
];
for (const [cur, key, n, want] of MOVES) {
  test(`paletteMove(${cur}, ${key}, ${n}) = ${want}`, () => {
    assert.equal(paletteMove(cur, key, n), want);
  });
}

test("paletteClamp keeps the cursor inside a narrowed list", () => {
  assert.equal(paletteClamp(7, 3), 2);
  assert.equal(paletteClamp(1, 3), 1);
  assert.equal(paletteClamp(4, 0), 0);
});

test("paletteFlatKeys orders rows section by section", () => {
  assert.deepEqual(
    paletteFlatKeys([{ rows: [{ key: "a" }, { key: "b" }] }, { rows: [] }, { rows: [{ key: "c" }] }]),
    ["a", "b", "c"],
  );
});

test("the app palettes supply sources only (the shell owns chrome and keys)", () => {
  for (const f of ["web/src/components/CommandPalette.tsx", "web2/src/components/CommandPalette.tsx"]) {
    const src = readFileSync(repo + f, "utf8");
    assert.match(src, /<CommandPaletteShell\b/, f);
    assert.doesNotMatch(src, /createPortal|"ArrowDown"|role="dialog"|framer-motion/, f);
  }
});
