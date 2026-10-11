import type { ReactNode } from "react";
import clsx from "clsx";
import { Check, Lock } from "lucide-react";
import { Icon } from "./Icon";
import {
  SETUP_STEP_STATE_LABEL,
  setupProgress,
  type SetupStepState,
} from "../lib/setupChecklist";

// SetupChecklist — a numbered, presentational setup checklist: each step in
// one of four states (done / current / todo / blocked, shared/lib/
// setupChecklist.ts) with an optional description and ONE action slot (a
// button or link that opens the right drawer). Built for the Agent Access
// overview (UI audit 2026-10-01 §6.2: "zero to this developer's agent can call
// this MCP server"), generic for any guided setup.
//
// Pure: the caller computes each step's state from its own data (use
// resolveCurrent from the lib when it only knows done / not-done) and passes
// the action nodes; this renders. No fetch, no router, no help coupling.

export interface SetupChecklistStep {
  id: string;
  title: ReactNode;
  description?: ReactNode;
  state: SetupStepState;
  /** The step's call to action (rendered for current / todo steps, and for a
   *  done step only when `actionWhenDone` is set). */
  action?: ReactNode;
  /** Render the action on a done step too (e.g. "Review"). */
  actionWhenDone?: boolean;
  /** Why a blocked step cannot start yet (shown instead of the action). */
  blockedReason?: ReactNode;
}

export function SetupChecklist({
  steps,
  title,
  showProgress = true,
  className,
}: {
  steps: SetupChecklistStep[];
  /** Optional heading rendered above the list with the progress count. */
  title?: ReactNode;
  /** Show "N of M done" beside the title (default true). */
  showProgress?: boolean;
  className?: string;
}) {
  const progress = setupProgress(steps);
  return (
    <div className={clsx("space-y-2", className)}>
      {(title || showProgress) && (
        <div className="flex items-baseline justify-between gap-3">
          {title ? <p className="text-[12.5px] font-semibold text-fg-1">{title}</p> : <span />}
          {showProgress && (
            <span className="font-mono text-[11px] text-fg-3">
              {progress.done} of {progress.total} done
            </span>
          )}
        </div>
      )}
      <ol className="space-y-1.5">
        {steps.map((s, i) => {
          const showAction = s.action && (s.state === "current" || s.state === "todo" || (s.state === "done" && s.actionWhenDone));
          return (
            <li
              key={s.id}
              aria-current={s.state === "current" ? "step" : undefined}
              className={clsx(
                "flex items-start gap-3 rounded-3 border px-3 py-2.5",
                s.state === "current" ? "border-accent/50 bg-accent-soft" : "border-line-2 bg-bg-2",
                s.state === "blocked" && "opacity-70",
              )}
            >
              <StepMarker index={i + 1} state={s.state} />
              <div className="min-w-0 flex-1">
                <p
                  className={clsx(
                    "text-[12.5px] font-medium",
                    s.state === "done" ? "text-fg-2" : "text-fg-1",
                  )}
                >
                  {s.title}
                </p>
                {s.description && <p className="mt-0.5 text-[11.5px] leading-snug text-fg-3">{s.description}</p>}
                {s.state === "blocked" && s.blockedReason && (
                  <p className="mt-0.5 text-[11px] leading-snug text-fg-3">{s.blockedReason}</p>
                )}
              </div>
              {showAction && <div className="shrink-0 self-center">{s.action}</div>}
            </li>
          );
        })}
      </ol>
    </div>
  );
}

function StepMarker({ index, state }: { index: number; state: SetupStepState }) {
  const label = SETUP_STEP_STATE_LABEL[state];
  if (state === "done") {
    return (
      <span
        title={label}
        className="mt-0.5 grid h-5 w-5 shrink-0 place-items-center rounded-pill border border-success/40 bg-success-soft text-success"
      >
        <Icon icon={Check} size={11} label={label} />
      </span>
    );
  }
  if (state === "blocked") {
    return (
      <span
        title={label}
        className="mt-0.5 grid h-5 w-5 shrink-0 place-items-center rounded-pill border border-line-2 bg-bg-3 text-fg-3"
      >
        <Icon icon={Lock} size={10} label={label} />
      </span>
    );
  }
  return (
    <span
      title={label}
      aria-label={`Step ${index}: ${label}`}
      className={clsx(
        "mt-0.5 grid h-5 w-5 shrink-0 place-items-center rounded-pill font-mono text-[10.5px]",
        state === "current" ? "bg-accent text-accent-on" : "border border-line-2 bg-bg-3 text-fg-2",
      )}
    >
      {index}
    </span>
  );
}
