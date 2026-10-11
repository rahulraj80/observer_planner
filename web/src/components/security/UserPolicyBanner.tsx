import { TriangleAlert } from "lucide-react";
import { Icon } from "@/components/primitives";
import { userPolicyNotice, type GuardUserPolicyProblem } from "@/lib/userPolicyBanner";

// UserPolicyBanner (T-323): the Security page's read-only banner for a user
// guard-policy.toml that does not load. The guard drops such a file (the
// built-in rules and the org policy stay in force), so without this the
// developer's own rules are silently off. Same in-page banner shape as the
// Settings/Sessions notices (rounded strip, icon, bold lead, detail), in the
// danger tone because rules the developer wrote are not being applied.
// Read-only: the one action is a link to the raw-TOML policy editor on the
// Rules tab, whose saves are lint-gated so a fix cannot itself be malformed.
export function UserPolicyBanner({
  problem,
  guardEnabled,
  onEdit,
}: {
  problem: GuardUserPolicyProblem | null | undefined;
  guardEnabled: boolean;
  onEdit: () => void;
}) {
  const notice = userPolicyNotice(problem, guardEnabled);
  if (!notice) return null;
  return (
    <div
      role="alert"
      data-testid="user-policy-banner"
      className="mb-4 flex items-start gap-3 rounded-3 border border-danger/30 bg-danger-soft/60 px-4 py-2.5 text-[11.5px]"
    >
      <Icon icon={TriangleAlert} size={14} className="mt-0.5 shrink-0 text-danger" />
      <div className="min-w-0 flex-1 text-fg-2">
        <b className="text-fg-1">{notice.title}</b> {notice.consequence}
        <div className="mt-1.5 break-all font-mono text-[11px] text-fg-1">{notice.location}</div>
        <pre className="mt-1 max-h-[96px] overflow-auto whitespace-pre-wrap break-words rounded-2 border border-danger/30 bg-bg-2 px-2.5 py-1.5 font-mono text-[11px] text-danger">
          {notice.error}
        </pre>
        <button
          type="button"
          onClick={onEdit}
          className="mt-2 inline-flex items-center gap-1.5 rounded-2 border border-line-3 bg-bg-2 px-2.5 py-1 text-[11px] font-medium text-fg-1 hover:bg-bg-3 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-ring"
        >
          Edit the policy file
        </button>
      </div>
    </div>
  );
}
