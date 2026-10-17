import clsx from "clsx";
import type { ReactNode } from "react";
import { CircleCheck, Info, OctagonAlert, TriangleAlert, type LucideIcon } from "lucide-react";
import { Icon } from "./Icon";
import type { CalloutTone } from "./copyModel";

// Callout: the one boxed message shape (docs/app-design-system.md
// "Emphasis"). Replaces the hand-rolled `border-warn/40 bg-warn/10` divs.
//
// When each tone is allowed:
//   info     an explanation or a deployment-wide fact worth stating once
//            ("How caps are enforced on this deployment"). The default.
//   warn     a condition the admin should act on, with the action named or
//            linked ("Re-issue recommended"). Never for an explanation.
//   danger   something is broken or about to be lost and needs action now.
//   success  a completed step worth confirming in place.
// At most ONE warn or danger callout per card, and one per page above the
// fold (`loudCalloutsOverBudget` in copyModel counts it). Only the icon and
// title carry the tone colour; the body stays neutral text, so a callout
// never becomes a paragraph painted in yellow.
export type CalloutProps = {
  tone?: CalloutTone;
  /** Short, sentence case. */
  title?: ReactNode;
  /** One line (copyModel COPY_LIMITS.callout); longer detail goes in an InfoDisclosure child. */
  children?: ReactNode;
  /** One action: a Button or a link, rendered at the end. */
  action?: ReactNode;
  /** Override the tone's glyph. */
  icon?: LucideIcon;
  className?: string;
};

const TONE: Record<CalloutTone, { box: string; accent: string; icon: LucideIcon; label: string }> = {
  info: { box: "border-info/30 bg-info-soft", accent: "text-info", icon: Info, label: "Note" },
  warn: { box: "border-warn/40 bg-warn-soft", accent: "text-warn", icon: TriangleAlert, label: "Warning" },
  danger: { box: "border-danger/40 bg-danger-soft", accent: "text-danger", icon: OctagonAlert, label: "Problem" },
  success: { box: "border-success/30 bg-success-soft", accent: "text-success", icon: CircleCheck, label: "Done" },
};

export function Callout({ tone = "info", title, children, action, icon, className }: CalloutProps) {
  const t = TONE[tone];
  return (
    // role="note", not "alert": a callout is static page content, and an
    // alert live region would interrupt a screen reader on every render. The
    // hidden tone label says what the colour says.
    <div role="note" className={clsx("flex items-start gap-2.5 rounded-2 border px-3 py-2", t.box, className)}>
      <Icon icon={icon ?? t.icon} size={15} className={clsx("mt-[1px] shrink-0", t.accent)} />
      <span className="sr-only">{t.label}: </span>
      <div className="min-w-0 flex-1 text-[12px] leading-snug text-fg-1">
        {title && <div className={clsx("font-semibold", t.accent)}>{title}</div>}
        {children && <div className={clsx("text-fg-2", title && "mt-0.5")}>{children}</div>}
      </div>
      {action && <div className="shrink-0 self-center">{action}</div>}
    </div>
  );
}
