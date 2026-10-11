import { useState } from "react";
import { Trash2 } from "lucide-react";
import { Button, ErrorState, InlineLoading, Pill, Select } from "@/components/primitives";
import { DiffView } from "@/components/DiffView";
import { fetchJSON, localConfirmHeaders } from "@/lib/api";
import { useApi } from "@/lib/useApi";
import {
  PROJECT_DEFAULT_TARGET,
  assignedProjects,
  changeSummary,
  projectAssignments,
  projectName,
  targetOptions,
  type ProjectProfileEdit,
  type ProjectProfilesResponse,
} from "@/lib/projectProfiles";

// ProjectProfilesCard - Settings > Profiles "Per-project assignments"
// (T-421 item 1).
//
// The dashboard form of `observer config set --project <root> profiles.*`:
// a project's own .observer/config.toml can point its traffic (all of it, one
// provider's, or one tool's) at a profile, and that wins over the global
// assignments above for sessions in that project. Backed by
// /api/config/project-profiles (owner-local, the profiles visibility guard,
// confirm token on writes). Only projects this node already knows are
// offered, and the daemon refuses any other path.
//
// Every write is preview first: "Preview" asks the daemon for the exact file
// change and shows it as a diff; "Write file" applies exactly that preview
// (the daemon refuses if the file changed in between). The daemon picks the
// change up on the project's next session, with no restart.

const ROUTE = "/api/config/project-profiles";

type Pending = { target: string; next: string; edit: ProjectProfileEdit } | null;

export function ProjectProfilesCard({ readOnly = false }: { readOnly?: boolean }) {
  const api = useApi<ProjectProfilesResponse>(ROUTE);
  const data = api.data ?? null;
  const projects = data?.projects ?? [];
  const [rootChoice, setRootChoice] = useState("");
  const [target, setTarget] = useState(PROJECT_DEFAULT_TARGET);
  const [profile, setProfile] = useState("");
  const [pending, setPending] = useState<Pending>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [saved, setSaved] = useState<string | null>(null);

  const root = rootChoice || projects[0]?.root || "";
  const row = projects.find((p) => p.root === root) ?? null;
  const rows = projectAssignments(row);
  const assigned = assignedProjects(data);
  const profiles = data?.profile_names ?? [];
  const profileChoice = profile || profiles[0] || "";
  const blocked = readOnly || busy || !row || Boolean(row.refusal) || Boolean(row.error);

  async function send(method: "PUT" | "DELETE", body: Record<string, unknown>) {
    return fetchJSON<ProjectProfileEdit>(ROUTE, undefined, {
      method,
      headers: await localConfirmHeaders(),
      body: JSON.stringify(body),
    });
  }

  async function preview(t: string, next: string) {
    setErr(null);
    setSaved(null);
    setBusy(true);
    try {
      const edit = await send(next ? "PUT" : "DELETE", { root, target: t, profile: next, preview: true });
      if (!edit.changed) {
        setPending(null);
        setSaved("Nothing to change: the project file already says that.");
        return;
      }
      setPending({ target: t, next, edit });
    } catch (e: unknown) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  async function commit() {
    if (!pending) return;
    setBusy(true);
    setErr(null);
    try {
      await send(pending.next ? "PUT" : "DELETE", {
        root,
        target: pending.target,
        profile: pending.next,
        base_sha: pending.edit.base_sha,
      });
      setSaved(`Wrote ${pending.edit.path}. New sessions in this project use it.`);
      setPending(null);
      api.reload();
    } catch (e: unknown) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  function pick(next: string) {
    setRootChoice(next);
    setPending(null);
    setSaved(null);
    setErr(null);
  }

  return (
    <div className="mt-4 rounded-3 border border-line-2 bg-bg-2 p-4">
      <h4 className="mb-1 text-[12px] font-semibold uppercase tracking-[0.06em] text-fg-1">
        Per-project assignments
      </h4>
      <p className="mb-3 text-[11px] leading-snug text-fg-3">
        Give one project its own profile. The assignment is written to that project&apos;s{" "}
        <code className="font-mono text-fg-2">.observer/config.toml</code> and wins over the
        assignments above for sessions in that project. Every write shows the exact file change
        first. Committing that file shares the choice with everyone who works in the repository.
      </p>

      {api.loading && !data && <InlineLoading label="Loading projects" />}
      {api.error && !data && (
        <ErrorState
          title="Couldn't load per-project assignments"
          error={api.error.message}
          onRetry={() => api.reload()}
        />
      )}

      {data && projects.length === 0 && (
        <p className="rounded-2 border border-dashed border-line-2 bg-bg-3/40 px-3 py-2 text-[11.5px] text-fg-3">
          No projects captured yet. A project appears here after its first session.
        </p>
      )}

      {data && projects.length > 0 && (
        <>
          {assigned.length > 0 && (
            <div className="mb-3 flex flex-wrap items-center gap-1.5 text-[11px] text-fg-3">
              <span>With their own assignments:</span>
              {assigned.map((p) => (
                <button
                  key={p.root}
                  type="button"
                  onClick={() => pick(p.root)}
                  title={p.root}
                  className="rounded-2 border border-line-2 bg-bg-3 px-2 py-0.5 font-mono text-fg-2 hover:text-fg-1"
                >
                  {projectName(p.root)}
                </button>
              ))}
            </div>
          )}

          <div className="mb-3 flex min-w-0 flex-wrap items-center gap-2">
            <span className="text-[11px] text-fg-3">Project</span>
            <Select
              mono
              aria-label="Project"
              value={root}
              onChange={(e) => pick(e.target.value)}
              disabled={busy}
              className="!w-auto min-w-0 max-w-full"
              options={projects.map((p) => ({ value: p.root, label: p.root }))}
            />
          </div>
          {data.truncated && (
            <p className="mb-2 text-[11px] text-fg-4">Showing the most recently active projects.</p>
          )}

          {row?.refusal && (
            <p className="mb-2 rounded-2 border border-warn/40 bg-warn-soft px-3 py-2 text-[11.5px] text-warn">
              This project&apos;s file cannot be written from here: {row.refusal}
            </p>
          )}
          {row?.error && (
            <p className="mb-2 rounded-2 border border-danger/30 bg-danger-soft px-3 py-2 text-[11.5px] text-danger">
              {row.error}. The daemon ignores this file until it is fixed, so fix it in an editor first.
            </p>
          )}

          {rows.length === 0 ? (
            <p className="mb-3 rounded-2 border border-dashed border-line-2 bg-bg-3/40 px-3 py-2 text-[11.5px] text-fg-3">
              {row?.has_file
                ? "This project's file assigns no profile: the assignments above apply."
                : "This project has no .observer/config.toml: the assignments above apply."}
            </p>
          ) : (
            <div className="mb-3 space-y-2">
              {rows.map((a) => (
                <div
                  key={a.target}
                  className="flex flex-wrap items-center gap-2 rounded-2 border border-line-1 bg-bg-3 px-3 py-2"
                >
                  <span className="min-w-[8rem] flex-1 text-[12px] text-fg-1">{a.label}</span>
                  <Pill variant="neutral">{a.profile}</Pill>
                  <Button
                    size="sm"
                    variant="secondary"
                    iconLeft={Trash2}
                    onClick={() => void preview(a.target, "")}
                    disabled={blocked}
                  >
                    Remove
                  </Button>
                </div>
              ))}
            </div>
          )}

          <div className="flex flex-wrap items-center gap-2 border-t border-line-1 pt-3">
            <Select
              aria-label="Traffic"
              value={target}
              onChange={(e) => setTarget(e.target.value)}
              disabled={blocked}
              className="!w-auto"
              options={targetOptions(data)}
            />
            <span className="text-[11px] text-fg-3">uses</span>
            <Select
              mono
              aria-label="Profile"
              value={profileChoice}
              onChange={(e) => setProfile(e.target.value)}
              disabled={blocked}
              className="!w-auto"
              options={profiles}
            />
            <Button
              variant="primary"
              size="sm"
              onClick={() => void preview(target, profileChoice)}
              disabled={blocked || !profileChoice}
            >
              Preview
            </Button>
          </div>

          {pending && (
            <div className="mt-3 rounded-2 border border-accent/40 bg-accent/10 px-3 py-2 text-[11.5px]">
              <p className="mb-2 font-mono text-fg-1">{changeSummary(row, pending.target, pending.next)}</p>
              <p className="mb-2 text-fg-3">
                {pending.edit.existed ? "Changes" : "Creates"}{" "}
                <span className="break-all font-mono text-fg-2">{pending.edit.path}</span>
                {pending.edit.existed ? " (the previous version is kept beside it as .bak)." : "."}
              </p>
              <DiffView diff={pending.edit.diff} maxHeight={220} />
              <div className="mt-2 flex flex-wrap items-center gap-2">
                <Button size="sm" variant={pending.next ? "primary" : "danger"} onClick={() => void commit()} disabled={busy}>
                  Write file
                </Button>
                <Button size="sm" variant="secondary" onClick={() => setPending(null)} disabled={busy}>
                  Cancel
                </Button>
              </div>
            </div>
          )}
          {saved && <p className="mt-2 text-[11.5px] text-success">{saved}</p>}
          {err && <p className="mt-2 text-[11.5px] text-danger">{err}</p>}
        </>
      )}
    </div>
  );
}
