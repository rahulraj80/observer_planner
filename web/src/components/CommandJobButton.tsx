import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import clsx from "clsx";
import { Button, ConfirmButton } from "@/components/primitives";
import { fetchJSON, localConfirmHeaders } from "@/lib/api";
import {
  commandJobPath,
  commandJobPollPath,
  jobSucceeded,
  jobOutcome,
  outputTail,
  redactDollarFigures,
  type CommandJobName,
} from "@/lib/commandJobs";
import { dollarsShown } from "@/lib/format";
import type { BackfillJob, BackfillJobsListResponse, BackfillRunResponse } from "@/lib/types";

// started_at lets a long job's card show how long it has been running.
export type JobSnap = Pick<BackfillJob, "id" | "status" | "output" | "error"> & { started_at?: string };

// useCommandJob runs one job and polls it until it finishes. `start` posts the
// request that begins it (the table route by default; a benchmark run passes
// its own). `mode` restores a job that is still running after the page was
// left and reopened (the registry lives in the daemon, not the page).
export function useCommandJob(opts: {
  mode: string;
  start: () => Promise<BackfillRunResponse>;
  onDone?: (job: JobSnap) => void;
}) {
  const [job, setJob] = useState<JobSnap | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let live = true;
    fetchJSON<BackfillJobsListResponse>("/api/jobs", { mode: opts.mode })
      .then((r) => {
        const latest = r.jobs?.[0];
        if (live && latest && latest.status === "running") {
          setJob({ id: latest.id, status: latest.status, output: latest.output, error: latest.error, started_at: latest.started_at });
        }
      })
      .catch(() => {
        // No restore is fine: the card simply starts idle.
      });
    return () => {
      live = false;
    };
  }, [opts.mode]);

  useEffect(() => {
    if (!job || job.status !== "running") return;
    const t = window.setInterval(() => {
      fetchJSON<BackfillJob>(commandJobPollPath(job.id))
        .then((j) => {
          const snap = { id: j.id, status: j.status, output: j.output, error: j.error, started_at: j.started_at };
          setJob(snap);
          if (j.status !== "running") opts.onDone?.(snap);
        })
        .catch(() => {
          // Transient poll failure: keep trying until a terminal status.
        });
    }, 1500);
    return () => window.clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [job?.id, job?.status]);

  async function run() {
    setBusy(true);
    setErr(null);
    try {
      const res = await opts.start();
      setJob({ id: res.job_id, status: "running", output: "", error: undefined, started_at: res.started_at });
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  return { job, err, busy, run, running: job?.status === "running" };
}

// startCommandJob posts the table route for one named job, carrying the
// owner-local confirm token the server requires (review M3-05).
export async function startCommandJob(name: CommandJobName): Promise<BackfillRunResponse> {
  return fetchJSON<BackfillRunResponse>(commandJobPath(name), undefined, {
    method: "POST",
    headers: await localConfirmHeaders(),
    body: "{}",
  });
}

// JobResult renders a job's outcome line and the tail of its output: the
// same lines the CLI prints, since the job IS the CLI command.
export function JobResult({
  job,
  err,
  doneText,
  lines = 8,
  className,
}: {
  job: JobSnap | null;
  err: string | null;
  doneText?: string;
  lines?: number;
  className?: string;
}) {
  const outcome = jobOutcome(job, doneText);
  // Token display (review M4-04): the CLI's dollar figures never reach the
  // pane; dollarsShown is the page-wide cost-display seam.
  const rawTail = outputTail(job?.output, lines);
  const tail = dollarsShown() ? rawTail : redactDollarFigures(rawTail);
  if (!outcome && !err) return null;
  return (
    <div className={clsx("min-w-0", className)}>
      {err && <p className="m-0 mt-2 text-[11.5px] text-danger">{err}</p>}
      {outcome && (
        <p
          role="status"
          className={clsx(
            "m-0 mt-2 text-[11.5px]",
            outcome.tone === "ok" && "text-success",
            outcome.tone === "danger" && "text-danger",
            outcome.tone === "info" && "text-fg-3",
          )}
        >
          {outcome.text}
        </p>
      )}
      {tail && (
        <pre className="m-0 mt-2 max-h-40 overflow-auto whitespace-pre-wrap break-words rounded-2 border border-line-2 bg-bg-3 px-3 py-2 font-mono text-caption text-fg-3">
          {tail}
        </pre>
      )}
    </div>
  );
}

// CommandJobButton is a button that runs one table job and shows its result
// under it. A job that writes asks for a second click first (the in-place
// ConfirmButton, with armedNote saying what is about to happen); a read-only
// job (requireConfirm={false}) runs on the first click.
export function CommandJobButton({
  job: name,
  children,
  runningLabel = "Running…",
  confirmLabel,
  armedNote,
  requireConfirm = true,
  variant = "secondary",
  size = "sm",
  doneText,
  onDone,
  disabled,
  className,
  outputLines = 8,
}: {
  outputLines?: number;
  job: CommandJobName;
  children: ReactNode;
  runningLabel?: string;
  confirmLabel?: ReactNode;
  armedNote?: ReactNode;
  requireConfirm?: boolean;
  variant?: "primary" | "secondary" | "ghost" | "danger";
  size?: "sm" | "md";
  doneText?: string;
  // onDone runs only when the job SUCCEEDED (review M4-10): a failed or
  // refused job must not trigger its success side effects (a restart banner
  // for a change that never happened).
  onDone?: () => void;
  disabled?: boolean;
  className?: string;
}) {
  const j = useCommandJob({
    mode: name,
    start: () => startCommandJob(name),
    onDone: (snap) => {
      if (jobSucceeded(snap)) onDone?.();
    },
  });
  const label = j.running ? runningLabel : children;
  return (
    <div className={clsx("min-w-0", className)}>
      {requireConfirm ? (
        <ConfirmButton
          variant={variant}
          armedVariant="primary"
          size={size}
          onConfirm={() => void j.run()}
          disabled={disabled || j.busy || j.running}
          confirmLabel={confirmLabel ?? "Run now?"}
          armedNote={armedNote}
        >
          {label}
        </ConfirmButton>
      ) : (
        <Button variant={variant} size={size} onClick={() => void j.run()} disabled={disabled || j.busy || j.running}>
          {label}
        </Button>
      )}
      <JobResult job={j.job} err={j.err} doneText={doneText} lines={outputLines} />
    </div>
  );
}
