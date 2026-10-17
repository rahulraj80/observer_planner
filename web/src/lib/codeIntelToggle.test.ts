import assert from "node:assert/strict";
import test from "node:test";

import {
  CODEINTEL_COPY,
  CODEINTEL_ENABLED_KEY,
  codeIntelState,
  codeIntelTogglePatch,
} from "./codeIntelToggle.ts";

test("codeIntelState reads only a real boolean", () => {
  const rows: [unknown, string][] = [
    [{ CodeIntel: { Enabled: false } }, "off"],
    [{ CodeIntel: { Enabled: true } }, "on"],
    [{ CodeIntel: {} }, "unknown"],
    [{ CodeIntel: { Enabled: "true" } }, "unknown"],
    [{}, "unknown"],
    [null, "unknown"],
    [undefined, "unknown"],
  ];
  for (const [cfg, want] of rows) {
    assert.equal(codeIntelState(cfg), want, JSON.stringify(cfg));
  }
});

test("codeIntelTogglePatch flips exactly codeintel.enabled", () => {
  assert.deepEqual(codeIntelTogglePatch("off"), { key: CODEINTEL_ENABLED_KEY, value: true, was: false });
  assert.deepEqual(codeIntelTogglePatch("on"), { key: CODEINTEL_ENABLED_KEY, value: false, was: true });
  assert.equal(codeIntelTogglePatch("unknown"), null);
});

test("copy: off offers Turn on, says why it is off, no em or en dashes", () => {
  assert.equal(CODEINTEL_COPY.off.action, "Turn on");
  assert.match(CODEINTEL_COPY.off.summary, /off by default/);
  assert.equal(CODEINTEL_COPY.on.action, "Turn off");
  for (const c of Object.values(CODEINTEL_COPY)) {
    for (const s of Object.values(c)) {
      assert.ok(!/[—–]/.test(s), `dash in ${s}`);
    }
    // No "run this command" dead end: the card is the control.
    assert.ok(!/observer (index|config)/.test(c.summary + c.armedNote), "copy points at a CLI command");
  }
});
