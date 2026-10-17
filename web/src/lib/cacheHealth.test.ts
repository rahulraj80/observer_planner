import assert from "node:assert/strict";
import test from "node:test";

import { gradingGateFailed } from "./cacheHealth.ts";

test("the cache page alarms only when the grading gate fails on enough events", () => {
  // The live shape that used to raise a yellow strip: huge diagnostic
  // counters, gate passed. No alarm.
  assert.equal(gradingGateFailed({ graded_events: 504_452, min_events_threshold: 200, gate_passed: true }), false);
  // Too few events to judge: no alarm either way.
  assert.equal(gradingGateFailed({ graded_events: 19, min_events_threshold: 200, gate_passed: false }), false);
  // Enough events and the gate failed: alarm.
  assert.equal(gradingGateFailed({ graded_events: 500, min_events_threshold: 200, gate_passed: false }), true);
  assert.equal(gradingGateFailed(null), false);
});
