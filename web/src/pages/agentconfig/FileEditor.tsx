import { useEffect, useState } from "react";
import { CodeEditor, ErrorState, InlineLoading, Pill, SlideOver } from "@/components/primitives";
import { useApi } from "@/lib/useApi";
import { countNoun, diagnosticsFor, editorLanguage, type RawView, type SaveOutcome } from "@/lib/agentConfig";
import { SaveFlowBody, SaveFlowFooter, useSaveFlow } from "./SaveFlow";
import { TrackedWarning } from "./PreviewPanel";

// FileEditor opens one vendor file as text (a settings file, an instruction
// file or a new skill / agent) in the shared CodeEditor. Secrets arrive as a
// placeholder; sending the placeholder back keeps the value on disk, moving
// it is refused. Observer's own spans (hook entries, its MCP server) are
// named in a banner and protected by the server's sanity check.

export type FileTarget = {
  tool: string;
  fileId: string;
  project: string;
  /** A concrete path for a glob row (an instruction file, a new skill). */
  path?: string;
  /** The file is tracked in git (a change can be committed). */
  tracked?: boolean;
  /** The starting text for a file that does not exist yet. */
  template?: string;
  title: string;
};

export function FileEditor({
  target,
  confirmToken,
  readOnly,
  readOnlyReason,
  onClose,
  onSaved,
}: {
  target: FileTarget | null;
  confirmToken: string;
  readOnly: boolean;
  readOnlyReason?: string;
  onClose: () => void;
  onSaved?: (o: SaveOutcome) => void;
}) {
  const raw = useApi<RawView>(
    target ? "/api/agent-config/file" : null,
    target
      ? { tool: target.tool, file_id: target.fileId, project: target.project || undefined, path: target.path }
      : undefined,
    [target?.tool, target?.fileId, target?.project, target?.path],
  );
  const [text, setText] = useState("");
  const [dirty, setDirty] = useState(false);
  const flow = useSaveFlow(confirmToken, (o) => {
    setDirty(false);
    raw.reload();
    onSaved?.(o);
  });

  useEffect(() => {
    if (!raw.data) return;
    const fresh = raw.data.state === "present" ? raw.data.text : (target?.template ?? raw.data.text);
    setText(fresh);
    setDirty(false);
    flow.reset();
    // Reload on a new file or a fresh read only.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [raw.data?.etag, raw.data?.path]);

  const v = raw.data;
  const editable = !readOnly && v !== null && v.editable && !v.raw_save_blocked;
  const req = target
    ? { tool: target.tool, file_id: target.fileId, project: target.project || undefined, path: target.path, raw: text }
    : null;

  return (
    <SlideOver
      open={target !== null}
      onClose={onClose}
      title={target?.title ?? ""}
      subtitle={v ? <code className="break-all font-mono text-[11px]">{v.path}</code> : undefined}
      width={920}
      bodyClassName="space-y-3 p-4"
      footer={
        req ? (
          <div className="border-t border-line-1 bg-bg-1 px-4 py-3">
            <SaveFlowFooter
              flow={flow}
              readOnly={!editable}
              readOnlyReason={readOnly ? readOnlyReason : v?.not_editable}
              disabled={!editable || (!dirty && v?.state === "present")}
              onPreview={() => void flow.runPreview(req)}
              onSave={() => void flow.runSave(req)}
            />
          </div>
        ) : undefined
      }
    >
      {raw.error && <ErrorState error={raw.error} onRetry={raw.reload} />}
      {raw.loading && !v && <InlineLoading label="Reading the file" />}
      {v && (
        <>
          <div className="flex flex-wrap items-center gap-2 text-[11.5px]">
            <Pill case="none">{v.format}</Pill>
            {v.state !== "present" && <Pill case="none">not created yet</Pill>}
            {(v.masked ?? []).length > 0 && (
              <Pill variant="warn" case="none" title={(v.masked ?? []).join(", ")}>
                {countNoun((v.masked ?? []).length, "hidden value")}
              </Pill>
            )}
            {readOnly && <Pill case="none">read-only</Pill>}
          </div>
          {target?.tracked && editable && <TrackedWarning />}
          {!v.editable && v.not_editable && <p className="text-[11.5px] text-fg-3">{v.not_editable}</p>}
          {v.raw_save_blocked && (
            <p className="text-[11.5px] text-warn">
              This file holds a credential Observer cannot hide in text, so it can be read here but saved only from the
              tool's own editor.
            </p>
          )}
          {(v.masked ?? []).length > 0 && (
            <p className="text-[11.5px] text-fg-3">
              Hidden values show as a placeholder. Leave a placeholder where it is to keep that value; edit the value in
              the tool's own file if it must change.
            </p>
          )}
          {(v.spans ?? []).filter((s) => s.live).length > 0 && (
            <div className="rounded-2 border border-line-2 bg-bg-3 p-2 text-[11.5px] text-fg-2">
              <div className="mb-1 font-medium text-fg-1">Observer's own entries in this file</div>
              <ul className="space-y-0.5">
                {(v.spans ?? [])
                  .filter((s) => s.live)
                  .map((s, i) => (
                    <li key={i}>
                      <code className="font-mono">{s.path || "(whole file)"}</code>
                      {s.entry ? <span className="font-mono"> {s.entry}</span> : null}: managed by Observer
                      {s.verb ? ` (${s.verb})` : ""}.{s.note ? ` ${s.note}` : ""}
                      {s.enforced ? " Removing or changing it here is refused." : ""}
                    </li>
                  ))}
              </ul>
            </div>
          )}
          <CodeEditor
            ariaLabel={`Contents of ${v.path}`}
            value={text}
            language={editorLanguage(v.format)}
            readOnly={!editable}
            diagnostics={diagnosticsFor(flow.preview?.findings)}
            onChange={(t) => {
              setText(t);
              setDirty(true);
            }}
            minHeight={280}
            maxHeight={520}
          />
          <SaveFlowBody flow={flow} />
        </>
      )}
    </SlideOver>
  );
}
