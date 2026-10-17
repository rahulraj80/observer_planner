import assert from "node:assert/strict";
import test from "node:test";

import { toolDoctorCommand, toolDoctorPath, toolDoctorSummary } from "./toolDoctor.ts";

// T-421 item 3: Connected tools' per-tool "Run checks".

test("toolDoctorPath encodes the tool id and toolDoctorCommand is the CLI twin", () => {
  assert.equal(toolDoctorPath("cursor"), "/api/health/doctor/tool?tool=cursor");
  assert.equal(toolDoctorPath("a b&c"), "/api/health/doctor/tool?tool=a%20b%26c");
  assert.equal(toolDoctorCommand("claude-code"), "observer doctor claude-code");
});

test("toolDoctorSummary tones by the worst status", () => {
  assert.equal(toolDoctorSummary(null), null);
  assert.deepEqual(toolDoctorSummary({ ok: 3, warn: 0, fail: 0 }), { tone: "ok", text: "3 ok, 0 warn, 0 fail: all checks passed" });
  assert.deepEqual(toolDoctorSummary({ ok: 2, warn: 1, fail: 0 }), { tone: "warn", text: "2 ok, 1 warn, 0 fail" });
  assert.deepEqual(toolDoctorSummary({ ok: 2, warn: 1, fail: 1 }), { tone: "danger", text: "2 ok, 1 warn, 1 fail" });
});
