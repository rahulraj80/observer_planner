import clsx from "clsx";
import { RefreshCw } from "lucide-react";
import { Button, InlineLoading } from "@/components/primitives";
import { CopyOnClick } from "@/components/CopyOnClick";
import { DoctorChecks } from "@/components/DoctorChecks";
import { useApi } from "@/lib/useApi";
import {
  toolDoctorCommand,
  toolDoctorPath,
  toolDoctorSummary,
  type ToolDoctorReport,
} from "@/lib/toolDoctor";
import { remediationButtons } from "@/lib/remediation";

// hasRemediationControls reports whether any finding in the report renders a
// dashboard remediation control.
function hasRemediationControls(report: ToolDoctorReport | null): boolean {
  return !!report && report.checks.some((c) => remediationButtons(c.remediations).length > 0);
}

// ToolDoctorPanel - Connected tools' per-tool "Run checks" (T-421 item 3):
// `observer doctor <tool>` in the dashboard. The daemon runs the same report
// builder the CLI prints (GET /api/health/doctor/tool, owner-local): the
// tool's capture check, control coverage, minimum runtime and every general
// check that names the tool, with the whole-database integrity probe deferred
// as the CLI defers it for one tool. It runs when the panel opens and on
// Re-run, never on a poll.
//
// Each finding's detail lines are its remediation hints, exactly as the CLI
// prints them. The terminal command is always shown beside the results, so a
// finding with no dashboard control of its own still has a way forward.

export function ToolDoctorPanel({ tool, onClose }: { tool: string; onClose: () => void }) {
  const run = useApi<ToolDoctorReport>(toolDoctorPath(tool));
  const report = run.data ?? null;
  const summary = toolDoctorSummary(report);
  const command = report?.command || toolDoctorCommand(tool);

  return (
    <div className="my-1.5 min-w-0 rounded-2 border border-line-2 bg-bg-3/40 px-3 py-2 text-[11.5px]">
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <span className="font-semibold text-fg-1">Checks for {tool}</span>
        {summary && (
          <span
            role="status"
            className={clsx(
              summary.tone === "ok" && "text-success",
              summary.tone === "warn" && "text-warn",
              summary.tone === "danger" && "text-danger",
            )}
          >
            {summary.text}
          </span>
        )}
        <span className="ml-auto inline-flex items-center gap-1.5">
          <Button variant="secondary" size="sm" iconLeft={RefreshCw} onClick={run.reload} disabled={run.loading}>
            Re-run
          </Button>
          <Button variant="ghost" size="sm" onClick={onClose}>
            Close
          </Button>
        </span>
      </div>
      {run.loading && !report && <InlineLoading label={`Running the ${tool} checks`} />}
      {run.error && !run.loading && (
        <p className="m-0 mb-2 text-danger">Couldn&apos;t run the checks here: {run.error.message}</p>
      )}
      {report && <DoctorChecks rows={report.checks} />}
      {/* A finding with a structured remediation (T-421 item 3) already links
          to the dashboard control under its row; the terminal command is the
          fallback only when no finding has one. */}
      {!hasRemediationControls(report) && (
        <p className="m-0 mt-2 text-fg-3">
          Same checks in a terminal:{" "}
          <CopyOnClick value={command} ariaLabel={`Copy ${command}`}>
            <code className="break-all font-mono text-fg-2">{command}</code>
          </CopyOnClick>
        </p>
      )}
    </div>
  );
}
