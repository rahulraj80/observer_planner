import { useState } from "react";
import { Trash2 } from "lucide-react";
import { Button, ErrorState, InlineLoading, Pill, Select } from "@/components/primitives";
import { fetchJSON, localConfirmHeaders } from "@/lib/api";
import { useApi } from "@/lib/useApi";
import {
  NOT_PROXIED_NOTE,
  assignmentRows,
  assignmentTarget,
  changePreview,
  isNoop,
  pickerTools,
  toolOptionLabel,
  type ProfileAssignmentsResponse,
} from "@/lib/profileAssign";

// ToolProfilesCard - Settings > Profiles "Per-tool assignments" (T-369).
//
// The dashboard form of `observer profile assign tool:NAME <profile>`: a
// tool assignment is more specific than the Anthropic / OpenAI one above and
// wins when the proxy can tell which tool owns the connection. Backed by
// /api/config/profile-assignments (GET table, PUT assign, DELETE remove;
// owner-local, the profiles visibility guard, confirm token on writes). The
// daemon validates the tool against the registry and the profile against the
// profile store, writes config.toml atomically and hot-reloads the proxy's
// profile router, so NEW sessions pick the change up with no restart.
//
// Every write is two clicks: the first arms it and states the exact change
// ("cline: its provider's profile -> codex-variant"), the second sends it.
// Arming runs the save pipeline's dry run (`?dry_run=1`, T-639): the confirm
// step names the config.toml lines the save would change, and the confirmed
// write carries the dry run's etag (`?base_etag=`), so a file changed in
// between is refused with 409 instead of overwritten unseen.

const ROUTE = "/api/config/profile-assignments";

type Pending = {
  tool: string;
  next: string;
  /** The etag the dry run was computed against (sent as base_etag). */
  etag: string;
  /** The dry run's line counts for the confirm step. */
  added: number;
  removed: number;
} | null;

type AssignDryRun = {
  config_etag: string;
  diff_stats?: { added: number; removed: number };
};

function assignBody(tool: string, next: string) {
  return next ? { target: assignmentTarget(tool), profile: next } : { target: assignmentTarget(tool) };
}

export function ToolProfilesCard({
  readOnly = false,
  onChanged,
}: {
  readOnly?: boolean;
  onChanged: () => void;
}) {
  const api = useApi<ProfileAssignmentsResponse>(ROUTE);
  const data = api.data ?? null;
  const [addTool, setAddTool] = useState("");
  const [addProfile, setAddProfile] = useState("");
  const [pending, setPending] = useState<Pending>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [saved, setSaved] = useState<string | null>(null);

  const rows = assignmentRows(data);
  const picker = pickerTools(data);
  const profiles = data?.profile_names ?? [];
  const toolChoice = addTool || picker[0]?.id || "";
  const profileChoice = addProfile || profiles[0] || "";

  async function arm(tool: string, next: string) {
    setErr(null);
    setSaved(null);
    if (isNoop(data, tool, next)) {
      setPending(null);
      return;
    }
    setBusy(true);
    try {
      const dry = await fetchJSON<AssignDryRun>(ROUTE, { dry_run: "1" }, {
        method: next ? "PUT" : "DELETE",
        headers: await localConfirmHeaders(),
        body: JSON.stringify(assignBody(tool, next)),
      });
      setPending({
        tool,
        next,
        etag: dry.config_etag ?? "",
        added: dry.diff_stats?.added ?? 0,
        removed: dry.diff_stats?.removed ?? 0,
      });
    } catch (e: unknown) {
      setPending(null);
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
      await fetchJSON<ProfileAssignmentsResponse>(ROUTE, { base_etag: pending.etag }, {
        method: pending.next ? "PUT" : "DELETE",
        headers: await localConfirmHeaders(),
        body: JSON.stringify(assignBody(pending.tool, pending.next)),
      });
      setSaved(
        pending.next
          ? `${pending.tool} now uses ${pending.next} for new sessions.`
          : `${pending.tool} falls back to its provider's profile for new sessions.`,
      );
      setPending(null);
      setAddTool("");
      api.reload();
      onChanged();
    } catch (e: unknown) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mt-4 rounded-3 border border-line-2 bg-bg-2 p-4">
      <h4 className="mb-1 text-[12px] font-semibold uppercase tracking-[0.06em] text-fg-1">
        Per-tool assignments
      </h4>
      <p className="mb-3 text-[11px] leading-snug text-fg-3">
        Give one tool its own profile. A tool assignment wins over the Anthropic or OpenAI
        assignment above whenever the proxy can tell which tool owns the connection; a tool
        with no assignment keeps its provider&apos;s profile. Changes apply to new sessions
        right away, with no restart.
      </p>

      {api.loading && !data && <InlineLoading label="Loading assignments" />}
      {api.error && !data && (
        <ErrorState
          title="Couldn't load per-tool assignments"
          error={api.error.message}
          onRetry={() => api.reload()}
        />
      )}

      {data && (
        <>
          {rows.length === 0 ? (
            <p className="mb-3 rounded-2 border border-dashed border-line-2 bg-bg-3/40 px-3 py-2 text-[11.5px] text-fg-3">
              No tool has its own profile yet: every tool uses its provider&apos;s profile.
            </p>
          ) : (
            <div className="mb-3 space-y-2">
              {rows.map((row) => (
                <div
                  key={row.tool}
                  className="flex flex-wrap items-center gap-2 rounded-2 border border-line-1 bg-bg-3 px-3 py-2"
                >
                  <span className="min-w-[8rem] flex-1 font-mono text-[12px] text-fg-1">
                    {row.tool}
                  </span>
                  {row.notProxied && (
                    <Pill variant="neutral" title={NOT_PROXIED_NOTE}>
                      not proxied
                    </Pill>
                  )}
                  {row.unknown && (
                    <Pill variant="warn" title="This tool id is not in the tool registry; the proxy never reports it, so the assignment has no effect. Remove it.">
                      unknown tool
                    </Pill>
                  )}
                  <Select
                    mono
                    aria-label={`Profile for ${row.tool}`}
                    value={pending?.tool === row.tool && pending.next ? pending.next : row.profile}
                    onChange={(e) => void arm(row.tool, e.target.value)}
                    disabled={readOnly || busy}
                    className="!w-auto"
                    options={profiles.includes(row.profile) ? profiles : [row.profile, ...profiles]}
                  />
                  <Button
                    size="sm"
                    variant="secondary"
                    iconLeft={Trash2}
                    onClick={() => void arm(row.tool, "")}
                    disabled={readOnly || busy}
                  >
                    Remove
                  </Button>
                </div>
              ))}
            </div>
          )}

          <div className="flex flex-wrap items-center gap-2 border-t border-line-1 pt-3">
            {picker.length > 0 ? (
              <>
                <Select
                  mono
                  aria-label="Tool"
                  value={toolChoice}
                  onChange={(e) => setAddTool(e.target.value)}
                  disabled={readOnly || busy}
                  className="!w-auto"
                  options={picker.map((t) => ({ value: t.id, label: toolOptionLabel(t) }))}
                />
                <span className="text-[11px] text-fg-3">uses</span>
                <Select
                  mono
                  aria-label="Profile"
                  value={profileChoice}
                  onChange={(e) => setAddProfile(e.target.value)}
                  disabled={readOnly || busy}
                  className="!w-auto"
                  options={profiles}
                />
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => void arm(toolChoice, profileChoice)}
                  disabled={readOnly || busy || !toolChoice || !profileChoice}
                >
                  Assign
                </Button>
              </>
            ) : (
              <span className="text-[11px] text-fg-3">Every known tool already has an assignment.</span>
            )}
          </div>

          {pending && (
            <div className="mt-3 flex flex-wrap items-center gap-2 rounded-2 border border-accent/40 bg-accent/10 px-3 py-2 text-[11.5px]">
              <span className="flex-1 font-mono text-fg-1">{changePreview(data, pending.tool, pending.next)}</span>
              <span className="text-[11px] text-fg-3">
                config.toml: +{pending.added} -{pending.removed} lines
              </span>
              <Button size="sm" variant={pending.next ? "primary" : "danger"} onClick={commit} disabled={busy}>
                {pending.next ? "Confirm assignment" : "Confirm removal"}
              </Button>
              <Button size="sm" variant="secondary" onClick={() => setPending(null)} disabled={busy}>
                Cancel
              </Button>
            </div>
          )}
          {saved && <p className="mt-2 text-[11.5px] text-success">{saved}</p>}
          {err && <p className="mt-2 text-[11.5px] text-danger">{err}</p>}
        </>
      )}
    </div>
  );
}
