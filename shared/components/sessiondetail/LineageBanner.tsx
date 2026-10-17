import { useState } from "react";
import clsx from "clsx";
import { ChevronDown, GitFork } from "lucide-react";
import { Icon, Pill, Tooltip } from "../../primitives";
import { lineageView, shortSid, type LineageInput } from "../../lib/sessionLineage";

// LineageBanner: a session's fork / sub-agent lineage, rendered ABOVE the
// drawer's tab strip (lineage is a fact about the session's identity, so
// hiding it behind a tab would make "this is a fork" discoverable only by
// accident). Promoted from the node dashboard (web/) so the org drawer
// (web2/) renders the same banner; each app resolves its own session row
// into a LineageInput (lib/sessionLineage.ts owns the words).
//
// PURE: fetches nothing, routes nowhere; navigation is the caller's
// onOpenSession. Renders nothing for a session with no lineage.

// lineagePillCls matches the Pill primitive's visual tokens so the clickable
// lineage badges read as chips, while remaining <button>s.
const lineagePillCls =
  "inline-flex items-center gap-1 rounded-pill border px-[7px] py-[1px] text-[10px] font-semibold leading-[1.4] tracking-[0.02em] transition-colors";

export type LineageBannerProps = LineageInput & {
  onOpenSession?: (id: string) => void;
};

export function LineageBanner({ onOpenSession, ...input }: LineageBannerProps) {
  const [expanded, setExpanded] = useState(false);
  const v = lineageView({ ...input, parentOpenable: Boolean(input.parentOpenable && onOpenSession) });
  if (!v.show) return null;
  const children = input.children ?? [];
  const parent = v.parent;

  return (
    <div className="mb-3 flex flex-col gap-2">
      {v.sidechainCount > 0 && !parent && children.length === 0 && (
        <div className="rounded-2 border border-accent/30 bg-accent-soft px-3 py-2 text-caption text-fg-2">
          Contains <span className="font-semibold text-fg-1">{v.sidechainCount}</span> sidechain action
          {v.sidechainCount === 1 ? "" : "s"} from inline sub-agents - see the System tab for the per-sub-agent
          breakdown.
        </div>
      )}
      {parent && (
        <div>
          {/* Enabled: a themed Tooltip. Disabled: the reason stays a native
              title (a disabled button fires no hover for the Tooltip). */}
          <Tooltip content={parent.openable ? parent.title : undefined}>
            <button
              type="button"
              disabled={!parent.openable}
              onClick={parent.openable ? () => onOpenSession!(parent.id) : undefined}
              title={parent.openable ? undefined : parent.title}
              className={clsx(
                lineagePillCls,
                parent.kind === "subagent"
                  ? "border-accent/30 bg-accent-soft text-accent"
                  : "border-info/30 bg-info-soft text-info",
                parent.openable
                  ? "cursor-pointer hover:brightness-110 focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]"
                  : "cursor-not-allowed opacity-55",
              )}
            >
              {parent.kind !== "subagent" && <Icon icon={GitFork} size={10} className="shrink-0" />}
              {parent.label}
            </button>
          </Tooltip>
        </div>
      )}
      {children.length > 0 && (
        <div className="rounded-2 border border-line-2 bg-bg-2 px-3 py-2">
          <button
            type="button"
            onClick={() => setExpanded((x) => !x)}
            aria-expanded={expanded}
            className="flex w-full items-center justify-between text-left text-caption font-medium text-fg-2 hover:text-fg-1 focus:outline-none"
          >
            <span>{v.childrenSummary}</span>
            <Icon
              icon={ChevronDown}
              size="xs"
              className={clsx("shrink-0 text-fg-3 transition-transform", expanded && "rotate-180")}
            />
          </button>
          {expanded && (
            <ul className="mt-2 flex flex-col gap-1">
              {children.map((c) => {
                const openable = !!onOpenSession;
                return (
                  <li key={c.id}>
                    <Tooltip content={openable ? `Open session ${c.id}` : undefined}>
                      <button
                        type="button"
                        disabled={!openable}
                        onClick={openable ? () => onOpenSession!(c.id) : undefined}
                        title={openable ? undefined : "Session navigation unavailable here"}
                        className={clsx(
                          "flex w-full items-center gap-2 rounded-1 px-2 py-1 text-left text-caption transition-colors",
                          openable
                            ? "cursor-pointer hover:bg-bg-3 focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]"
                            : "cursor-default",
                        )}
                      >
                        <Pill variant={c.kind === "subagent" ? "accent" : "info"}>{c.kind}</Pill>
                        <span className="font-mono text-fg-2">{shortSid(c.id)}…</span>
                        {c.meta ? (
                          <span className="ml-auto shrink-0 font-mono text-[10px] text-fg-3">{c.meta}</span>
                        ) : null}
                      </button>
                    </Tooltip>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
