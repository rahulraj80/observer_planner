import { useState } from "react";
import { FolderGit2, Pencil } from "lucide-react";
import {
  Button,
  Card,
  EmptyState,
  ErrorState,
  InlineLoading,
  Pill,
  Table,
  TruncatedPath,
} from "@/components/primitives";
import { useApi } from "@/lib/useApi";
import {
  SCOPE_LABEL,
  STATE_LABEL,
  formatValue,
  type Access,
  type SettingView,
  type ToolView,
} from "@/lib/agentConfig";
import { SettingEditor } from "./ToolsTab";

// ProjectsTab: the project layer of one tool in one project (plan section 7
// item 3): the project files (shared, tracked with the repository, vs local,
// personal) and every setting a project file sets, with the file it lands in.

const PROJECT_SCOPES = new Set(["project_shared", "project_local"]);

export function ProjectsTab({
  tool,
  project,
  access,
  confirmToken,
}: {
  tool: string;
  project: string;
  access: Access | null;
  confirmToken: string;
}) {
  const view = useApi<ToolView>(
    tool && project ? "/api/agent-config/tool" : null,
    { tool, project: project || undefined },
    [tool, project],
  );
  const [editing, setEditing] = useState<SettingView | null>(null);

  if (!project) {
    return (
      <EmptyState
        title="Choose a project"
        body="Pick a project above to see the settings files this tool reads inside it and the values they set."
      />
    );
  }
  if (view.error && !view.data) return <ErrorState error={view.error} onRetry={view.reload} />;
  if (!view.data) return <InlineLoading label="Reading the project's settings" />;
  const v = view.data;
  const files = v.files.filter((f) => PROJECT_SCOPES.has(f.scope) && f.kind !== "instructions" && f.kind !== "skill" && f.kind !== "agent" && f.kind !== "rule" && f.kind !== "command");
  const projectFileIds = new Set(files.map((f) => f.id));
  const settings = v.settings.filter((s) => (s.values ?? []).some((x) => projectFileIds.has(x.file_id)));

  return (
    <div className="space-y-4">
      <Card
        title="Project files"
        icon={FolderGit2}
        sub="Shared files travel with the repository; local files are personal and stay out of git."
        stale={view.isStale}
      >
        {files.length === 0 ? (
          <p className="text-[11.5px] text-fg-3">This tool reads no project settings files.</p>
        ) : (
          <Table
            minWidth={620}
            head={
              <tr>
                <th className="px-2 py-1.5 text-left">Placement</th>
                <th className="px-2 py-1.5 text-left">File</th>
                <th className="px-2 py-1.5 text-left">State</th>
              </tr>
            }
          >
            {files.map((f) => (
              <tr key={f.id} className="border-t border-line-1 align-top">
                <td className="whitespace-nowrap px-2 py-1.5 text-fg-2">{SCOPE_LABEL[f.scope]}</td>
                <td className="max-w-[420px] px-2 py-1.5">
                  {f.path ? <TruncatedPath value={f.path} className="font-mono text-[11px] text-fg-1" /> : <span className="text-fg-4">{f.path_status}</span>}
                  <div className="mt-0.5 flex flex-wrap gap-1">
                    {f.tracked && (
                      <Pill variant="warn" case="none" title="A change here shows up in git status and can be committed.">
                        commits with the repo
                      </Pill>
                    )}
                    {f.gitignored && <Pill case="none">gitignored</Pill>}
                  </div>
                </td>
                <td className="px-2 py-1.5">
                  <Pill variant={f.state === "present" ? "success" : "neutral"} case="none">
                    {STATE_LABEL[f.state]}
                  </Pill>
                </td>
              </tr>
            ))}
          </Table>
        )}
      </Card>
      <Card title="Set in this project" sub="Settings a project file sets, and which file they land in.">
        {settings.length === 0 ? (
          <p className="text-[11.5px] text-fg-3">No project file of this tool sets anything yet.</p>
        ) : (
          <ul className="divide-y divide-line-1" aria-label="Project settings">
            {settings.map((s) => (
              <li key={s.id} className="flex min-w-0 flex-col gap-1 py-2 sm:flex-row sm:items-start sm:gap-3">
                <div className="min-w-0 flex-1">
                  <code className="break-all font-mono text-[12px] text-fg-1">{s.path}</code>
                  <ul className="mt-0.5 space-y-0.5 text-[11.5px]">
                    {(s.values ?? [])
                      .filter((x) => projectFileIds.has(x.file_id))
                      .map((x) => (
                        <li key={x.file_id} className="flex min-w-0 flex-wrap gap-x-2">
                          <span className="text-fg-3">{SCOPE_LABEL[x.scope]}</span>
                          <code className="min-w-0 break-all font-mono text-fg-2">
                            {x.secret_present ? "Set (hidden)" : formatValue(x.value)}
                          </code>
                        </li>
                      ))}
                  </ul>
                </div>
                <div className="sm:shrink-0">
                  <Button size="sm" variant="ghost" iconLeft={Pencil} onClick={() => setEditing(s)}>
                    {access?.read_only ? "Details" : "Edit"}
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </Card>
      <SettingEditor
        view={v}
        setting={editing}
        project={project}
        readOnly={access?.read_only ?? false}
        readOnlyReason={access?.reason}
        confirmToken={confirmToken}
        onClose={() => setEditing(null)}
        onSaved={() => view.reload()}
      />
    </div>
  );
}
