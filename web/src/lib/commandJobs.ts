import type { BackfillJob } from "./types.ts";

// Pure helpers for the dashboard's command jobs (the CLI actions that used to
// be CLI-only: score, patterns, calibration, summarize, pricing sync, the MCP
// relay verbs, benchmark runs). The server runs the `observer` command itself
// as a subprocess (internal/intelligence/dashboard/command_jobs.go); this
// module only names the routes and phrases a job's state, so every card that
// runs one says the same thing.

// COMMAND_JOBS is the closed set of job names, one per server table row.
export const COMMAND_JOBS = [
  "score",
  "summarize",
  "patterns",
  "calibration",
  "pricing-sync",
  "mcp-wrap",
  "mcp-unwrap",
  "mcp-relay-enable",
  "mcp-relay-disable",
  "mcp-doctor",
  "doctor-integrity",
  "reclaim-cleanup",
] as const;

export type CommandJobName = (typeof COMMAND_JOBS)[number];

// commandJobPath is the POST route that starts a job.
export function commandJobPath(name: CommandJobName): string {
  return `/api/jobs/${name}`;
}

// CANCELABLE_JOBS is the jobs whose server row registers a cancel route
// (T-421 item 2): a long read the operator may stop.
export const CANCELABLE_JOBS: readonly CommandJobName[] = ["doctor-integrity", "reclaim-cleanup"];

// commandJobCancelPath is the POST route that stops a running cancelable job.
export function commandJobCancelPath(name: CommandJobName): string {
  return `/api/jobs/${name}/cancel`;
}

// commandJobPollPath is the GET route that polls one job.
export function commandJobPollPath(id: string): string {
  return `/api/jobs/${encodeURIComponent(id)}`;
}

// DOLLAR_FIGURE matches a dollar amount the CLI prints: "$0.12", "~$5",
// "$1,234.50", "-$0.01", "12.50 USD".
const DOLLAR_FIGURE = /[~-]?\$\s?\d[\d,]*(?:\.\d+)?|\b\d[\d,]*(?:\.\d+)?\s?USD\b/g;

// COST_HIDDEN_TEXT replaces a dollar figure in job output under token display.
export const COST_HIDDEN_TEXT = "(cost hidden)";

// redactDollarFigures removes every dollar amount from CLI output (review
// M4-04): under the org's token display (D-013 / D-179, pending or unverified
// included) a job pane must not print the dollar estimates, caps and spend the
// CLI writes, the same rule the rest of each page follows.
export function redactDollarFigures(text: string): string {
  return text.replace(DOLLAR_FIGURE, COST_HIDDEN_TEXT);
}

// outputTail keeps the last n non-empty-trailing lines of a job's output.
export function outputTail(output: string | undefined, n = 8): string {
  if (!output) return "";
  return output.split("\n").slice(-n - 1).join("\n").trim();
}

// jobElapsed renders how long a job has run as "m:ss" (or "h:mm:ss"), from
// its server start time to now (ms). An unknown or future start reads "0:00".
export function jobElapsed(startedAt: string | undefined, now: number): string {
  const start = startedAt ? Date.parse(startedAt) : NaN;
  const total = Number.isFinite(start) ? Math.max(0, Math.floor((now - start) / 1000)) : 0;
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const sec = String(total % 60).padStart(2, "0");
  return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${sec}` : `${m}:${sec}`;
}

// jobSucceeded reports a job that finished successfully (review M4-10): only
// then may a card run its success side effects (a restart banner, a refresh
// that assumes the change happened).
export function jobSucceeded(job: Pick<BackfillJob, "status"> | null | undefined): boolean {
  return job?.status === "done";
}

export type JobOutcome = { tone: "info" | "ok" | "danger"; text: string };

// jobOutcome phrases a job's state as one line. A failed job carries the
// server's own error (the exit code, or the spawn error).
export function jobOutcome(
  job: Pick<BackfillJob, "status" | "error"> | null | undefined,
  doneText = "Done.",
): JobOutcome | null {
  if (!job) return null;
  switch (job.status) {
    case "running":
      return { tone: "info", text: "Running…" };
    case "done":
      return { tone: "ok", text: doneText };
    case "canceled":
      return { tone: "info", text: "Canceled before it finished." };
    default:
      return { tone: "danger", text: job.error ? `Failed: ${job.error}` : "Failed." };
  }
}
