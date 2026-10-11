// setupChecklist — the pure model behind the SetupChecklist primitive
// (shared/primitives/SetupChecklist.tsx). No React, so it is unit-tested with
// node --test (web2/src/lib/setupChecklist.test.ts).
//
// A checklist is an ordered list of steps, each in one closed state. The
// caller decides each step's done-ness from its own data (that derivation is
// app logic and lives in the app, e.g. the Agent Access overview); this
// module only answers the presentation questions every checklist asks.

/** The closed step-state vocabulary.
 *  - done: complete.
 *  - current: the step to do next (usually exactly one; see resolveCurrent).
 *  - todo: not started, not blocked.
 *  - blocked: cannot be started until something else happens (say why). */
export type SetupStepState = "done" | "current" | "todo" | "blocked";

export interface SetupStepFacts {
  id: string;
  state: SetupStepState;
}

/** setupProgress counts done steps; complete is true when every step is done
 *  (an empty checklist is not complete: there is nothing to show as done). */
export function setupProgress(steps: readonly SetupStepFacts[]): { done: number; total: number; complete: boolean } {
  const done = steps.filter((s) => s.state === "done").length;
  return { done, total: steps.length, complete: steps.length > 0 && done === steps.length };
}

/** resolveCurrent returns the states with exactly one "current" step when the
 *  caller only knows done / not-done: the FIRST step that is neither done nor
 *  blocked becomes current, any other "current" is demoted to "todo". A
 *  caller that already marks one step current keeps it (the first one wins). */
export function resolveCurrent<T extends SetupStepFacts>(steps: readonly T[]): T[] {
  let found = false;
  return steps.map((s) => {
    if (s.state === "done" || s.state === "blocked") return s;
    if (!found) {
      found = true;
      return s.state === "current" ? s : { ...s, state: "current" as const };
    }
    return s.state === "current" ? { ...s, state: "todo" as const } : s;
  });
}

/** stepLabel is the accessible / tooltip word for a state. */
export const SETUP_STEP_STATE_LABEL: Record<SetupStepState, string> = {
  done: "Done",
  current: "Next step",
  todo: "Not started",
  blocked: "Blocked",
};
