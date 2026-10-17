// cacheHealth: the Cache page's one engine-health alarm rule. Pure, so the
// node test runner covers it.

/** The fields of /api/cache/health the alarm rule reads. */
export type GradingGate = {
  graded_events: number;
  min_events_threshold: number;
  gate_passed: boolean;
};

/**
 * gradingGateFailed is the one alarm condition, read straight off the
 * server's verdict (gate_passed uses the same thresholds as
 * `observer cache-health`): enough graded events AND the gate failed.
 * Below the grading minimum there is nothing to judge yet, so nothing
 * alarms. The engine's labelling diagnostics (inconsistent rewrites,
 * bucket mismatches, a dominant cause) never alarm on their own: they grow
 * with history on every install.
 */
export function gradingGateFailed(health: GradingGate | null | undefined): boolean {
  if (!health) return false;
  return health.graded_events >= health.min_events_threshold && !health.gate_passed;
}
