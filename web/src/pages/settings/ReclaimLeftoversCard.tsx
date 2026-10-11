import { useEffect, useState } from "react";
import { Trash2 } from "lucide-react";
import { Button, ChartShell, ConfirmButton, Table } from "@/components/primitives";
import { JobResult, startCommandJob, useCommandJob } from "@/components/CommandJobButton";
import { fetchJSON, localConfirmHeaders } from "@/lib/api";
import { commandJobCancelPath, jobElapsed } from "@/lib/commandJobs";

// ReclaimLeftoversCard - Settings > Storage (T-493): the original database
// `observer archive reclaim` renamed aside (<db>.pre-reclaim-<stamp>, with its
// -wal / -shm). A reclaim never deletes it, and it is as large as the
// database was before the reclaim (a forgotten 35.7 GB one filled the disk on
// 2026-09-26). This card lists them and deletes them on request by running
// `observer archive reclaim-cleanup --delete` as a job: the CLI first checks
// the database now in place with PRAGMA quick_check and refuses while a
// reclaim is still writing its copy, so the card can never delete what the
// command would refuse to. Nothing here runs on its own.

/** One reclaim leftover as GET /api/storage reports it. */
export type ReclaimLeftover = {
  stamp: string;
  bytes: number;
  files: { name: string; bytes: number }[];
};

const JOB = "reclaim-cleanup" as const;

export function ReclaimLeftoversCard({
  leftovers,
  fmtBytes,
  onDone,
}: {
  leftovers: ReclaimLeftover[];
  fmtBytes: (n: number) => string;
  onDone?: () => void;
}) {
  // A finished run (either way: a refusal leaves the list as it was, a
  // success empties it) refreshes the storage report.
  const { job, err, busy, run, running } = useCommandJob({
    mode: JOB,
    start: () => startCommandJob(JOB),
    onDone: () => onDone?.(),
  });
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

  const total = leftovers.reduce((n, l) => n + l.bytes, 0);
  if (leftovers.length === 0 && !job) return null;

  return (
    <ChartShell
      title="Reclaim leftovers"
      icon={Trash2}
      sub={leftovers.length > 0 ? `${fmtBytes(total)} kept aside by a database reclaim` : "nothing kept aside"}
    >
      <p className="m-0 text-[11.5px] leading-relaxed text-fg-3">
        A reclaim (<code className="font-mono">observer archive reclaim --run</code>) keeps the original database
        beside the compacted one and never deletes it. Once the daemon has run cleanly on the compacted database,
        delete the original here. The delete first checks the database now in use (a full read of the file, minutes
        on a large database) and refuses if that check fails, so the only good copy is never removed.
      </p>
      {leftovers.length > 0 && (
        <Table minWidth={320}>
          {leftovers.flatMap((l) =>
            l.files.map((f) => (
              <tr key={f.name} className="border-b border-line-1 last:border-0">
                <td className="break-all py-1 pr-2 font-mono text-[11px] text-fg-2">{f.name}</td>
                <td className="py-1 text-right text-[11px] text-fg-3">{fmtBytes(f.bytes)}</td>
              </tr>
            )),
          )}
        </Table>
      )}
      <div className="mt-3 flex flex-wrap items-center gap-3">
        {leftovers.length > 0 && (
          <ConfirmButton
            variant="secondary"
            armedVariant="danger"
            size="sm"
            onConfirm={() => void run()}
            disabled={busy || running}
            confirmLabel={`Delete ${fmtBytes(total)}?`}
            armedNote="Checks the database in use first; deletes the leftovers only if it passes. This cannot be undone."
          >
            {running ? "Checking, then deleting…" : "Verify and delete"}
          </ConfirmButton>
        )}
        {running && (
          <>
            <span className="text-[11.5px] text-fg-3" role="status">
              Running for {jobElapsed(job?.started_at, now)}. Stopping before the check finishes deletes nothing.
            </span>
            <Button variant="danger" size="sm" onClick={() => void cancel()} disabled={canceling}>
              {canceling ? "Stopping…" : "Stop"}
            </Button>
          </>
        )}
      </div>
      {cancelErr && <p className="m-0 mt-2 text-[11.5px] text-danger">{cancelErr}</p>}
      <JobResult job={job} err={err} doneText="Leftovers deleted." lines={10} />
    </ChartShell>
  );
}
