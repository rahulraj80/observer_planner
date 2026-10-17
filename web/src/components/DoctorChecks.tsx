import clsx from "clsx";
import type { LucideIcon } from "lucide-react";
import { VOCAB_ICONS } from "@shared/lib/vocabIcons";
import { Link } from "react-router-dom";
import { Icon, Tooltip } from "@/components/primitives";
import { remediationButtons } from "@/lib/remediation";
import type { DoctorReport } from "@/lib/types";

// DoctorChecks renders `observer doctor` check rows: one row per check with
// its status glyph, id, message and the indented detail lines (the
// remediation hints, the same text the CLI prints). The one renderer for the
// Health page's whole report and Connected tools' per-tool report (T-421
// item 3), so a change to how a finding reads (or a structured remediation
// control) lands on both.

// CHECK_MARK maps a doctor check status to its glyph, tone and accessible
// name - one row per status, so a new status is one table row (an unknown
// value falls back to the fail row rather than rendering nothing). The
// glyphs are VOCAB_ICONS.healthCheck (the one glyph owner); the colours match
// CHECK_STATUS in @/lib/vocabTones (ok success, warn warn, fail danger).
const CHECK_MARK: Record<string, { icon: LucideIcon; tone: string; label: string }> = {
  ok: { icon: VOCAB_ICONS.healthCheck.ok, tone: "text-success", label: "ok" },
  warn: { icon: VOCAB_ICONS.healthCheck.warn, tone: "text-warn", label: "warning" },
  fail: { icon: VOCAB_ICONS.healthCheck.fail, tone: "text-danger", label: "failed" },
};

export type DoctorCheckRow = DoctorReport["checks"][number];

export function DoctorChecks({
  rows,
  muted,
}: {
  rows: DoctorCheckRow[];
  /** muted reports whether a row renders in the quiet (informational) chrome. */
  muted?: (c: DoctorCheckRow) => boolean;
}) {
  return (
    <div className="space-y-1.5">
      {rows.map((c) => {
        const mark = CHECK_MARK[c.status] ?? CHECK_MARK.fail;
        return (
          <div
            key={c.name}
            className={clsx("rounded-2 border border-line-1 px-3 py-2", muted?.(c) ? "bg-bg-1 text-fg-3" : "bg-bg-2")}
          >
            <div className="flex min-w-0 items-baseline gap-2">
              <span className="flex w-4 shrink-0 justify-center self-center">
                <Icon icon={mark.icon} size="sm" label={mark.label} className={mark.tone} />
              </span>
              <span className="shrink-0 font-mono text-[11.5px] text-fg-1">{c.name}</span>
              <span className="min-w-0 flex-1 break-words text-[11.5px] text-fg-2">{c.message}</span>
            </div>
            {c.details && c.details.length > 0 && (
              <ul className="ml-6 mt-1 list-disc space-y-0.5 text-[11px] text-fg-3">
                {c.details.map((line, i) => (
                  <li key={i} className="whitespace-pre-wrap break-words">
                    {line}
                  </li>
                ))}
              </ul>
            )}
            <RemediationLinks check={c} />
          </div>
        );
      })}
    </div>
  );
}

// RemediationLinks renders a check's structured fixes (T-421 item 3): each
// links to the dashboard control that does what the quoted command does; the
// command stays in the tooltip for terminal users. Both the server's label
// and link come from internal/remedy's one table.
function RemediationLinks({ check }: { check: DoctorCheckRow }) {
  const buttons = remediationButtons(check.remediations);
  if (buttons.length === 0) return null;
  return (
    <div className="ml-6 mt-1.5 flex flex-wrap gap-1.5" data-testid="doctor-remediations">
      {buttons.map((r) => (
        <Tooltip
          key={`${r.href}|${r.label}`}
          content={r.command ? `CLI: ${r.command}` : "No single command: use this control"}
          maxWidth={360}
        >
          <Link
            to={r.href}
            className="inline-flex items-center rounded-2 border border-line-2 bg-bg-3 px-2 py-0.5 text-[11px] font-medium text-accent hover:border-accent/50 hover:text-accent-strong"
          >
            {r.label}
          </Link>
        </Tooltip>
      ))}
    </div>
  );
}
