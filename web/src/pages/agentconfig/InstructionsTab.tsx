import { useState } from "react";
import { BookOpen, FilePlus2, Pencil } from "lucide-react";
import {
  Button,
  Card,
  EmptyState,
  ErrorState,
  InlineLoading,
  Input,
  Pill,
  Table,
  TruncatedPath,
} from "@/components/primitives";
import { useApi } from "@/lib/useApi";
import { SCOPE_LABEL, type Access, type InstructionFile } from "@/lib/agentConfig";
import { asCallError, newInstructionPath } from "./api";
import { FileEditor, type FileTarget } from "./FileEditor";

// InstructionsTab: a tool's instruction files (memory files, rules, skills,
// subagents), at user and project scope. A skill or subagent is created
// from a small template with the front matter the tool needs; the sanity
// check refuses one without a name and description.

function template(kind: string, name: string): string {
  if (kind === "skill" || kind === "agent") {
    return `---\nname: ${name}\ndescription: Say when the tool should use this ${kind === "skill" ? "skill" : "subagent"}.\n---\n\n`;
  }
  return "";
}

export function InstructionsTab({
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
  const files = useApi<{ files: InstructionFile[] }>(
    tool ? "/api/agent-config/instructions" : null,
    { tool, project: project || undefined },
    [tool, project],
  );
  const [target, setTarget] = useState<FileTarget | null>(null);
  const [names, setNames] = useState<Record<string, string>>({});
  const [err, setErr] = useState("");
  const readOnly = access?.read_only ?? false;

  if (!tool) return <EmptyState title="Choose a tool" body="Pick an AI tool above to see its instruction files." />;
  if (files.error && !files.data) return <ErrorState error={files.error} onRetry={files.reload} />;
  if (!files.data) return <InlineLoading label="Finding instruction files" />;

  const rows = files.data.files.filter((f) => !f.new_name);
  const creators = files.data.files.filter((f) => f.new_name);

  async function create(f: InstructionFile) {
    const name = (names[f.file_id] ?? "").trim();
    setErr("");
    try {
      const { path } = await newInstructionPath(tool, f.file_id, name, project);
      setTarget({
        tool,
        fileId: f.file_id,
        project,
        path,
        template: template(f.kind, name),
        title: `New ${f.kind}`,
      });
    } catch (e) {
      setErr(asCallError(e).message);
    }
  }

  return (
    <div className="space-y-4">
      <Card
        title="Instruction files"
        icon={BookOpen}
        sub="Memory files, rules, skills and subagents this tool reads. Project files apply to the selected project only."
        stale={files.isStale}
      >
        {rows.length === 0 ? (
          <p className="text-[11.5px] text-fg-3">No instruction files found for this tool here.</p>
        ) : (
          <Table
            minWidth={640}
            head={
              <tr>
                <th className="px-2 py-1.5 text-left">Kind</th>
                <th className="px-2 py-1.5 text-left">Scope</th>
                <th className="px-2 py-1.5 text-left">File</th>
                <th className="px-2 py-1.5 text-right" aria-label="Open" />
              </tr>
            }
          >
            {rows.map((f) => (
              <tr key={`${f.file_id}-${f.path}`} className="border-t border-line-1 align-top">
                <td className="px-2 py-1.5 text-fg-2">{f.kind}</td>
                <td className="whitespace-nowrap px-2 py-1.5 text-fg-3">{SCOPE_LABEL[f.scope]}</td>
                <td className="max-w-[420px] px-2 py-1.5">
                  <TruncatedPath value={f.path} className="font-mono text-[11px] text-fg-1" />
                  <div className="mt-0.5 flex flex-wrap gap-1">
                    {!f.exists && <Pill case="none">not created yet</Pill>}
                    {f.name && <Pill case="none">{f.name}</Pill>}
                    {f.tracked && <Pill case="none">commits with the repo</Pill>}
                    {f.parse_error && (
                      <Pill variant="danger" case="none" title={f.parse_error}>
                        front matter does not parse
                      </Pill>
                    )}
                  </div>
                </td>
                <td className="px-2 py-1.5 text-right">
                  <Button
                    size="sm"
                    variant="ghost"
                    iconLeft={Pencil}
                    onClick={() =>
                      setTarget({ tool, fileId: f.file_id, project, path: f.path, tracked: f.tracked, title: `${f.kind} file` })
                    }
                  >
                    {readOnly ? "View" : f.exists ? "Edit" : "Create"}
                  </Button>
                </td>
              </tr>
            ))}
          </Table>
        )}
      </Card>
      {creators.length > 0 && !readOnly && (
        <Card title="New skill or subagent" icon={FilePlus2} sub="Name it, then write it in the editor. Nothing is written until you save.">
          <div className="space-y-2">
            {creators.map((f) => (
              <div key={f.file_id} className="flex flex-wrap items-end gap-2">
                <Input
                  label={`${SCOPE_LABEL[f.scope]} ${f.kind}`}
                  placeholder="name"
                  mono
                  value={names[f.file_id] ?? ""}
                  onChange={(e) => setNames({ ...names, [f.file_id]: e.target.value })}
                  className="w-full sm:w-64"
                />
                <Button
                  size="sm"
                  iconLeft={FilePlus2}
                  disabled={(names[f.file_id] ?? "").trim() === ""}
                  onClick={() => void create(f)}
                >
                  Start
                </Button>
                <span className="min-w-0 break-all font-mono text-[11px] text-fg-4">{f.glob}</span>
              </div>
            ))}
            {err && (
              <p role="alert" className="text-[11.5px] text-danger">
                {err}
              </p>
            )}
          </div>
        </Card>
      )}
      <FileEditor
        target={target}
        confirmToken={confirmToken}
        readOnly={readOnly}
        readOnlyReason={access?.reason}
        onClose={() => setTarget(null)}
        onSaved={() => files.reload()}
      />
    </div>
  );
}
