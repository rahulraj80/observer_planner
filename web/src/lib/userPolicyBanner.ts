// userPolicyBanner.ts - pure copy logic for the Security page's invalid
// user-policy banner (T-323). The daemon reports, on /api/guard/summary, a
// user guard-policy.toml that exists but does not load: the guard dropped
// it, so the developer's own rules are OFF while the built-in rules and the
// org policy stay in force. No fetch, no DOM; the banner component is a thin
// renderer over this.

// GuardUserPolicyProblem mirrors internal/intelligence/dashboard's
// guardUserPolicyJSON. Absent from the summary when the file loads.
export type GuardUserPolicyProblem = {
  path: string;
  line?: number;
  error: string;
  // The organization chose fail_closed for this managed node (D-221): the
  // enforced gated hooks are BLOCKED while the file does not load.
  fail_closed?: boolean;
};

export type UserPolicyNotice = {
  title: string;
  // "path, line N" (or just the path when the line is unknown).
  location: string;
  error: string;
  consequence: string;
};

export function userPolicyNotice(
  problem: GuardUserPolicyProblem | null | undefined,
  guardEnabled: boolean,
): UserPolicyNotice | null {
  if (!problem || !problem.path) return null;
  const location = problem.line && problem.line > 0 ? `${problem.path}, line ${problem.line}` : problem.path;
  if (problem.fail_closed && guardEnabled) {
    return {
      title: "Your guard policy file does not load, and your organization blocks guarded calls until it does.",
      location,
      error: problem.error.trim() || "unknown error",
      consequence:
        "Your organization set this machine to fail closed: every tool call and prompt the guard enforces is blocked until the file loads. Fix the file to unblock your agents.",
    };
  }
  return {
    title: "Your guard policy file does not load, so its rules are off.",
    location,
    error: problem.error.trim() || "unknown error",
    consequence: guardEnabled
      ? "Built-in rules and the org policy stay in force. Fix the file to turn your own rules back on."
      : "The guard is off today. When it is on, this file's rules will be dropped until it is fixed.",
  };
}
