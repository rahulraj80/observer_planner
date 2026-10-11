import { FlaskConical, History, RefreshCw, ScanSearch, ShieldCheck, FileText } from "lucide-react";
import { useEffect, useState } from "react";
import { Button, ChartShell, ConfirmButton, Input, Pill, SegmentedControl } from "@/components/primitives";
import { TitleWithHelp } from "@/components/HelpInd";
import { useApi } from "@/lib/useApi";
import { fetchJSON } from "@/lib/api";
import { decisionTone } from "@shared/lib/guardCatalog";
import { JOB_STATUS } from "@shared/lib/sessionVocab";
import { VocabPill } from "@shared/lib/vocabPill";
import {
  guardJobLabel,
  guardTestEnforceLine,
  guardTestHeadline,
  type GuardJob,
  type GuardJobsView,
  type GuardTestResponse,
} from "@/lib/guardTools";

// Guard tools (T-154): the Security page's twins of `observer guard test`,
// `guard rescan`, `guard compile [--diff]` and `guard mcp scan | report`.
// The test is answered in process by the shared internal/guardtest; the rest
// run as jobs in the shared registry (the Compliance evidence card's
// pattern), each argv from a closed server-side table. A kind that changes
// state carries the confirm token its route's GET minted.

const BOX = "rounded-2 border border-line-2 bg-bg-3 p-3 text-[11.5px]";

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

// useGuardJobs drives one guard job route: its GET view (kinds + confirm
// token) and the job it last started, polled until it settles.
function useGuardJobs(route: "/api/guard/jobs/policy" | "/api/guard/jobs/mcp") {
  const view = useApi<GuardJobsView>(route);
  const [job, setJob] = useState<GuardJob | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!job || job.status !== "running") return;
    const t = window.setInterval(() => {
      fetchJSON<GuardJob>(`/api/backfill/jobs/${job.id}`)
        .then(setJob)
        .catch(() => {
          // Transient (a daemon restart loses the in-memory registry).
        });
    }, 1000);
    return () => window.clearInterval(t);
  }, [job]);

  async function start(kind: string, extra: Record<string, string> = {}) {
    setBusy(kind);
    setError("");
    try {
      const writes = view.data?.kinds.find((k) => k.kind === kind)?.writes;
      const headers: Record<string, string> = { "Content-Type": "application/json" };
      if (writes && view.data?.confirm_token) headers["X-Observer-Confirm"] = view.data.confirm_token;
      const r = await fetchJSON<{ job_id: string }>(route, undefined, {
        method: "POST",
        headers,
        body: JSON.stringify({ kind, ...extra }),
      });
      setJob({ id: r.job_id, mode: `guard:${kind}`, status: "running", output: "" });
    } catch (e) {
      setError(errText(e));
    } finally {
      setBusy(null);
    }
  }

  const running = job?.status === "running";
  return { view, job, busy, running, error, start };
}

function JobResult({ job, failNote }: { job: GuardJob | null; failNote?: string }) {
  if (!job) return null;
  return (
    <div className={`mt-3 ${BOX}`} aria-live="polite">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium text-fg-1">{guardJobLabel(job.mode)}</span>
        <VocabPill vocab="jobStatus" table={JOB_STATUS} value={job.status} />
      </div>
      {job.error && (
        <div className="mt-1.5 text-danger">
          {job.error}
          {failNote ? ` ${failNote}` : ""}
        </div>
      )}
      {job.output && (
        <pre className="mt-1.5 max-h-56 overflow-auto whitespace-pre-wrap font-mono text-[10.5px] leading-relaxed text-fg-2">
          {job.output}
        </pre>
      )}
    </div>
  );
}

// TestCommandCard is `observer guard test`: dry-run one command or file write
// against the effective policy, in a project this observer has captured.
export function TestCommandCard({ projects }: { projects: string[] }) {
  const [kind, setKind] = useState<"command" | "file">("command");
  const [target, setTarget] = useState("");
  const [project, setProject] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [res, setRes] = useState<GuardTestResponse | null>(null);

  async function run(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      setRes(
        await fetchJSON<GuardTestResponse>("/api/guard/test", undefined, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ kind, target, project_root: project }),
        }),
      );
    } catch (err) {
      setError(errText(err));
      setRes(null);
    } finally {
      setBusy(false);
    }
  }

  const r = res?.report;
  return (
    <ChartShell
      title={<TitleWithHelp text="Test a command" helpId="card.security_guard_test" />}
      icon={FlaskConical}
      sub="Dry-run a shell command or a file write against today's policy and see the verdict in your current mode and in enforce. Nothing runs and nothing is recorded."
    >
      <form className="space-y-3" onSubmit={(e) => void run(e)}>
        <div className="flex flex-wrap items-center gap-2">
          <SegmentedControl<"command" | "file">
            size="sm"
            options={[
              { value: "command", label: "Shell command" },
              { value: "file", label: "File write" },
            ]}
            value={kind}
            onChange={setKind}
          />
          <select
            aria-label="Project the dry run happens in"
            className="min-w-0 max-w-full rounded-2 border border-line-2 bg-bg-3 px-2 py-1 text-caption text-fg-1"
            value={project}
            onChange={(e) => setProject(e.target.value)}
          >
            <option value="">Outside any project (home directory)</option>
            {projects.map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-2 sm:flex-row">
          <Input
            className="min-w-0 flex-1 font-mono"
            aria-label={kind === "command" ? "Command to test" : "Path to test a write to"}
            placeholder={kind === "command" ? "git push --force origin main" : "~/.ssh/config"}
            value={target}
            maxLength={4096}
            onChange={(e) => setTarget(e.target.value)}
          />
          <Button type="submit" size="sm" variant="primary" iconLeft={ShieldCheck} loading={busy} disabled={!target.trim()}>
            Test
          </Button>
        </div>
      </form>
      {error && <div className="mt-2 text-[11.5px] text-danger">{error}</div>}
      {res && r && (
        <div className={`mt-3 space-y-1.5 ${BOX}`} aria-live="polite">
          <div className="flex flex-wrap items-center gap-2">
            <Pill variant={decisionTone(r.decision)} case="none">
              {r.decision}
            </Pill>
            <span className="font-medium text-fg-1">{guardTestHeadline(r)}</span>
          </div>
          {r.reason && <div className="text-fg-2">{r.reason}</div>}
          {r.advice && <div className="text-fg-3">Advice: {r.advice}</div>}
          <div className={res.mode_changes_verdict ? "text-warn" : "text-fg-3"}>{guardTestEnforceLine(res)}</div>
          <div className="break-all font-mono text-[10.5px] text-fg-3">
            {r.kind} {JSON.stringify(r.target)} in {r.project_root || "the home directory"}
          </div>
        </div>
      )}
    </ChartShell>
  );
}

// ApplyPolicyCard is what a policy change needs after the save: `guard
// rescan` (record the verdicts history would have earned) and `guard compile
// [--diff]` (the native AI-client rules the policy compiles into, R-204).
export function ApplyPolicyCard() {
  const jobs = useGuardJobs("/api/guard/jobs/policy");
  const [period, setPeriod] = useState<"24h" | "168h" | "720h">("168h");
  const compileOn = jobs.view.data?.compile_enabled !== false;
  const locked = jobs.running || jobs.busy !== null || !jobs.view.data;

  return (
    <ChartShell
      title={<TitleWithHelp text="Apply policy changes" helpId="card.security_apply_policy" />}
      icon={RefreshCw}
      sub="After a rule change: re-judge captured history so the verdict timeline reflects today's rules, and keep the AI clients' own permission rules in step with the policy so they block even when the observer is down."
    >
      <div className="space-y-3 text-[11.5px]">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-medium text-fg-1">Re-judge history</span>
          <SegmentedControl<"24h" | "168h" | "720h">
            size="sm"
            options={[
              { value: "24h", label: "24h" },
              { value: "168h", label: "7d" },
              { value: "720h", label: "30d" },
            ]}
            value={period}
            onChange={setPeriod}
          />
          <ConfirmButton
            size="sm"
            variant="secondary"
            armedVariant="primary"
            iconLeft={History}
            disabled={locked}
            loading={jobs.busy === "rescan"}
            armedNote="Records a guard verdict for every captured action the live guard missed in this window. Re-running never duplicates rows."
            onConfirm={() => void jobs.start("rescan", { period })}
          >
            Re-judge
          </ConfirmButton>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-medium text-fg-1">Native rules</span>
          <Button size="sm" variant="secondary" iconLeft={ScanSearch} disabled={locked || !compileOn} loading={jobs.busy === "compile-diff"} onClick={() => void jobs.start("compile-diff")}>
            Check for drift
          </Button>
          <ConfirmButton
            size="sm"
            variant="secondary"
            armedVariant="primary"
            iconLeft={ShieldCheck}
            disabled={locked || !compileOn}
            loading={jobs.busy === "compile"}
            armedNote="Writes the compiled rules into each supported AI client's own permission config (Claude Code permissions.deny/ask, OpenCode permission.bash). Your own entries are never removed."
            onConfirm={() => void jobs.start("compile")}
          >
            Write native rules
          </ConfirmButton>
          {!compileOn && (
            <span className="text-fg-3">
              Native rule compilation is off. Turn on Compile on init in Settings &gt; Guard &gt; Native dialects to use it.
            </span>
          )}
        </div>
      </div>
      <JobResult
        job={jobs.job}
        failNote={jobs.job?.mode === "guard:compile-diff" ? "A failed drift check means the native rules are out of step with the policy; the output below lists what differs." : undefined}
      />
      {jobs.error && <div className="mt-2 text-[11.5px] text-danger">{jobs.error}</div>}
    </ChartShell>
  );
}

// MCPToolsBar is `guard mcp scan` and `guard mcp report` above the MCP
// server inventory.
export function MCPToolsBar({ onScanned }: { onScanned: () => void }) {
  const jobs = useGuardJobs("/api/guard/jobs/mcp");
  const locked = jobs.running || jobs.busy !== null || !jobs.view.data;
  const done = jobs.job?.mode === "guard:mcp-scan" && jobs.job.status !== "running";
  useEffect(() => {
    if (done) onScanned();
  }, [done]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <div className="mb-3">
      <div className="flex flex-wrap items-center gap-2">
        <ConfirmButton
          size="sm"
          variant="secondary"
          armedVariant="primary"
          iconLeft={ScanSearch}
          disabled={locked}
          loading={jobs.busy === "mcp-scan"}
          armedNote="Reads every supported AI client's MCP config now, pins new servers and records any drift. The daemon also does this whenever a config changes."
          onConfirm={() => void jobs.start("mcp-scan")}
        >
          Scan now
        </ConfirmButton>
        <Button size="sm" variant="secondary" iconLeft={FileText} disabled={locked} loading={jobs.busy === "mcp-report"} onClick={() => void jobs.start("mcp-report")}>
          Compliance summary
        </Button>
      </div>
      <JobResult job={jobs.job} />
      {jobs.error && <div className="mt-2 text-[11.5px] text-danger">{jobs.error}</div>}
    </div>
  );
}
