import assert from "node:assert/strict";
import test from "node:test";

import { capturePillView } from "./capturePill.ts";
import { CAPTURE_DOT } from "./liveSignals.ts";

test("capturePillView: one row per setup status, never 'Paused' for a missing tool", () => {
  const rows: [string | null | undefined, string, string][] = [
    ["oauth_ready", "Active", "active"],
    ["api_key_ready", "Active", "active"],
    // Node 1, 2026-10-06: Claude Code is not installed there; the pill read
    // "Paused" while every other tool was being captured.
    ["claude_not_installed", "No Claude Code here", "absent"],
    ["something_new", "Not routed", "paused"],
    [undefined, "Checking", "absent"],
    [null, "Checking", "absent"],
  ];
  for (const [status, label, state] of rows) {
    const v = capturePillView(status);
    assert.equal(v.label, label, String(status));
    assert.equal(v.state, state, String(status));
    assert.notEqual(v.label, "Paused");
    assert.ok(v.tooltip.length > 0);
    assert.doesNotMatch(v.label + v.tooltip, /[\u2013\u2014]/);
    assert.ok(CAPTURE_DOT[v.state], `CAPTURE_DOT has a "${v.state}" state`);
  }
  assert.match(capturePillView("something_new").tooltip, /something_new/);
});

test("capturePillView: no pill at all when Claude Code is not on this machine", () => {
  // T-803: the pill read "No Claude Code" beside a dashboard full of Claude
  // Code sessions captured from another OS. Nothing to report here: hidden.
  assert.equal(capturePillView("claude_not_installed").hidden, true);
  for (const s of ["oauth_ready", "api_key_ready", "something_new", undefined]) {
    assert.ok(!capturePillView(s).hidden, String(s));
  }
});
