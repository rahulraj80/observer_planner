import { useEffect, useState } from "react";
import { DatabaseZap } from "lucide-react";
import { Button, ChartShell, ConfirmButton } from "@/components/primitives";
import { JobResult, startCommandJob, useCommandJob } from "@/components/CommandJobButton";
import { fetchJSON, localConfirmHeaders } from "@/lib/api";
import { commandJobCancelPath, jobElapsed } from "@/lib/commandJobs";

// IntegrityCheckCard - Settings > Health "Full database integrity check"
// (T-421 item 2): `observer doctor db --integrity` as a dashboard action.
//
// The doctor above skips the whole-database PRAGMA quick_check on a database
// over [observer.db].integrity_check_max_gb (and a per-tool doctor defers it),
// because it reads every page of the file. This card runs it on demand as a
// background job through the command-jobs runner (the CLI command itself, as
// a subprocess), shows how long it has been running, and can stop it. It only
// reads the database, and it never runs on its own: only this button starts
// it.

const JOB = "doctor-integrity" as const;

export function IntegrityCheckCard() {
  const { job, err, busy, run, running } = useCommandJob({ mode: JOB, start: () => startCommandJob(JOB) });
  const [cancelErr, setCancelErr] = useState<string | null>(null);
  const [canceling, setCanceling] = useState(false);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (!running) return;
    const t = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(t);
  }, [running]);

  useEffect(() => {
    if (!running) setCanceling(false);
  }, [running]);

  async function cancel() {
    if (!job) return;
    setCancelErr(null);
    setCanceling(true);
    try {
      await fetchJSON(commandJobCancelPath(JOB), undefined, {
        method: "POST",
        headers: await localConfirmHeaders(),
        body: JSON.stringify({ job_id: job.id }),
      });
    } catch (e) {
      setCanceling(false);
      setCancelErr(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <ChartShell
      title="Full database integrity check"
      icon={DatabaseZap}
      sub="The whole-database PRAGMA quick_check the checks above skip on a large database. Runs only when you start it."
    >
      <p className="m-0 text-[11.5px] leading-relaxed text-fg-3">
        It reads every page of observer.db, so it takes as long as reading the whole file: minutes on
        a multi-GB database (about 12 minutes was measured on a 27 GB one), with the disk busy the whole
        time, which can slow other work on this machine. It only reads: nothing is repaired, rewritten
        or deleted.
        You can stop it at any point.
      </p>
      <div className="mt-3 flex flex-wrap items-center gap-3">
        <ConfirmButton
          variant="secondary"
          armedVariant="primary"
          size="sm"
          onConfirm={() => void run()}
          disabled={busy || running}
          confirmLabel="Start the full check?"
          armedNote="Reads the whole database file now. On a large database this keeps the disk busy for minutes."
        >
          {running ? "Checking…" : "Run full integrity check"}
        </ConfirmButton>
        {running && (
          <>
            <span className="text-[11.5px] text-fg-3" role="status">
              Running for {jobElapsed(job?.started_at, now)}. quick_check reports no progress of its own
              until it finishes.
            </span>
            <Button variant="danger" size="sm" onClick={() => void cancel()} disabled={canceling}>
              {canceling ? "Stopping…" : "Stop"}
            </Button>
          </>
        )}
      </div>
      {cancelErr && <p className="m-0 mt-2 text-[11.5px] text-danger">{cancelErr}</p>}
      <JobResult job={job} err={err} doneText="Integrity check finished." lines={12} />
    </ChartShell>
  );
}
