// Pins the Windows-home control's reading (windowsHomes.ts, T-421 item 4).
import { test } from "node:test";
import assert from "node:assert/strict";
import { cliEquivalent, defaultHome, needsChoice, previewLines, windowsHomesVisible, type WindowsHomeTarget } from "./windowsHomes.ts";

const ambiguous: WindowsHomeTarget = {
  target: "codex-windows",
  subdir: ".codex",
  candidates: [
    { home: "/mnt/c/Users/a", owned: false },
    { home: "/mnt/c/Users/b", owned: false },
  ],
  flag: "--windows-codex-home",
  command: "observer init --windows-codex-home <path>",
};

test("shown only on a WSL daemon with targets", () => {
  assert.equal(windowsHomesVisible({ wsl: false, targets: [ambiguous] }), false);
  assert.equal(windowsHomesVisible({ wsl: true, targets: [] }), false);
  assert.equal(windowsHomesVisible({ wsl: true, targets: [ambiguous] }), true);
  assert.equal(windowsHomesVisible(null), false);
});

test("never guesses among several homes; pre-selects the one init would pick", () => {
  assert.equal(defaultHome(ambiguous), "");
  assert.equal(needsChoice(ambiguous), true);
  const resolved = { ...ambiguous, resolved: "/mnt/c/Users/b" };
  assert.equal(defaultHome(resolved), "/mnt/c/Users/b");
  assert.equal(needsChoice(resolved), false);
});

test("the CLI equivalent names the chosen home", () => {
  assert.equal(cliEquivalent(ambiguous, "/mnt/c/Users/b"), "observer init --windows-codex-home /mnt/c/Users/b");
  assert.equal(cliEquivalent(ambiguous, ""), "observer init --windows-codex-home <path>");
});

test("an empty preview says so", () => {
  assert.deepEqual(previewLines({ target: "codex-windows", home: "/h", dry_run: true, lines: [""] }), [
    "codex-windows: nothing to write for this home",
  ]);
  assert.deepEqual(previewLines(null), []);
});
