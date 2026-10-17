import assert from "node:assert/strict";
import test from "node:test";

import { guiAvailabilityName } from "./guiLaunchCopy.ts";

// T-333: the not-installed / could-not-check copy names a GUI row by its
// label, never its launch id; a terminal tool keeps its id.
const rows = [
  { id: "zed-ide", label: "Zed" },
  { id: "cursor-ide", label: "Cursor" },
  { id: "blank-label", label: "  " },
];

test("a GUI row is named by its label", () => {
  assert.equal(guiAvailabilityName("zed-ide", rows), "Zed");
  assert.equal(guiAvailabilityName("cursor-ide", rows), "Cursor");
});

test("a terminal tool keeps its id", () => {
  assert.equal(guiAvailabilityName("codex", rows), "codex");
});

test("a GUI row with no label falls back to its id", () => {
  assert.equal(guiAvailabilityName("blank-label", rows), "blank-label");
});
