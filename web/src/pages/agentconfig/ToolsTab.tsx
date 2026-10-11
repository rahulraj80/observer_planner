import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { FileText, Pencil, SlidersHorizontal } from "lucide-react";
import {
  Button,
  Card,
  EmptyState,
  ErrorState,
  InlineLoading,
  Input,
  Pill,
  Select,
  SinglePicker,
  SlideOver,
  Table,
  Textarea,
  Toggle,
  TruncatedPath,
} from "@/components/primitives";
import { isModelSetting, settingValueOptions } from "@/lib/pickerOptions";
import { useModelOptions } from "@/lib/usePickerSources";
import { useApi } from "@/lib/useApi";
import {
  layerLabel,
  SCOPE_LABEL,
  STATE_LABEL,
  countNoun,
  effectiveText,
  filterSettings,
  inputText,
  keyPath,
  parseInput,
  writableFiles,
  type Access,
  type FileView,
  type SettingView,
  type ToolView,
} from "@/lib/agentConfig";
import { canAskToEdit } from "@/lib/configEditRequest";
import { configEditRequestLink } from "@/lib/requestEntry";
import { FileEditor, type FileTarget } from "./FileEditor";
import { SaveFlowBody, SaveFlowFooter, useSaveFlow } from "./SaveFlow";
import { TrackedWarning } from "./PreviewPanel";

// ToolsTab: one tool's settings files (every scope it reads), each catalogued
// setting's effective value and the file that decides it, and the editors.

const SETTINGS_PAGE = 60;

function fileStateTone(f: FileView): "success" | "neutral" | "warn" | "danger" {
  if (f.parse_error) return "danger";
  if (f.state === "present") return "success";
  if (f.state === "unreadable") return "warn";
  return "neutral";
}

export function ToolsTab({
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
  const view = useApi<ToolView>(tool ? "/api/agent-config/tool" : null, { tool, project: project || undefined }, [
    tool,
    project,
  ]);
  const [fileTarget, setFileTarget] = useState<FileTarget | null>(null);
  const [editing, setEditing] = useState<SettingView | null>(null);
  const [q, setQ] = useState("");
  const [onlySet, setOnlySet] = useState(false);
  const [onlyCommon, setOnlyCommon] = useState(false);
  const [onlyDanger, setOnlyDanger] = useState(false);
  const [limit, setLimit] = useState(SETTINGS_PAGE);

  const v = view.data;
  const readOnly = access?.read_only ?? false;
  const rows = useMemo(
    () => (v ? filterSettings(v.settings, { q, onlySet, onlyCommon, onlyDanger }) : []),
    [v, q, onlySet, onlyCommon, onlyDanger],
  );

  if (!tool) return <EmptyState title="Choose a tool" body="Pick an AI tool above to see its settings files." />;
  if (view.error && !v) return <ErrorState error={view.error} onRetry={view.reload} />;
  if (!v) return <InlineLoading label="Reading the tool's settings" />;

  const settingsFiles = v.files.filter((f) => f.kind !== "instructions" && f.kind !== "skill" && f.kind !== "agent" && f.kind !== "rule" && f.kind !== "command");
  // Project files need a project: without one they are named once below
  // instead of as rows with no path. Lowest precedence first, as the
  // card says.
  const structured = settingsFiles
    .filter((f) => f.path_status !== "needs_project")
    .sort((a, b) => a.precedence - b.precedence);
  const needProject = settingsFiles.length - structured.length;

  return (
    <div className="space-y-4">
      <Card
        title="Settings files"
        icon={FileText}
        sub="Every file this tool reads, lowest precedence first. A later file overrides an earlier one."
        stale={view.isStale}
      >
        <Table
          minWidth={720}
          head={
            <tr>
              <th className="px-2 py-1.5 text-left">Scope</th>
              <th className="px-2 py-1.5 text-left">File</th>
              <th className="px-2 py-1.5 text-left">State</th>
              <th className="px-2 py-1.5 text-left">Notes</th>
              <th className="px-2 py-1.5 text-right" aria-label="Open" />
            </tr>
          }
        >
          {structured.map((f) => (
            <tr key={f.id} className="border-t border-line-1 align-top">
              <td className="whitespace-nowrap px-2 py-1.5 text-fg-2">{SCOPE_LABEL[f.scope]}</td>
              <td className="max-w-[360px] px-2 py-1.5">
                {f.path ? (
                  <TruncatedPath value={f.path} className="font-mono text-[11px] text-fg-1" />
                ) : (
                  <span className="text-fg-4">{f.reason || f.path_status}</span>
                )}
              </td>
              <td className="px-2 py-1.5">
                <Pill variant={fileStateTone(f)} case="none">
                  {f.parse_error ? "Does not parse" : STATE_LABEL[f.state]}
                </Pill>
              </td>
              <td className="px-2 py-1.5 text-[11px] text-fg-3">
                <div className="flex flex-wrap gap-1">
                  {(f.spans ?? []).some((s) => s.live) && <Pill case="none">Observer entries</Pill>}
                  {(f.secrets ?? []).length > 0 && (
                    <Pill variant="warn" case="none">
                      {countNoun((f.secrets ?? []).length, "secret")} present
                    </Pill>
                  )}
                  {f.tracked && f.path && <Pill case="none">commits with the repo</Pill>}
                  {f.write_needs === "admin_root" && <Pill case="none">needs admin</Pill>}
                  {f.write_needs === "never" && <Pill case="none">vendor owned</Pill>}
                  {(f.uncatalogued ?? []).length > 0 && (
                    <Pill case="none" title={(f.uncatalogued ?? []).join(", ")}>
                      {countNoun((f.uncatalogued ?? []).length, "unknown key")}
                    </Pill>
                  )}
                </div>
                {f.not_editable && f.state !== "never_read" && <div className="mt-1">{f.not_editable}</div>}
              </td>
              <td className="px-2 py-1.5 text-right">
                {f.path && f.state !== "never_read" && f.path_status === "resolved" && (
                  <Button
                    size="sm"
                    variant="ghost"
                    iconLeft={Pencil}
                    onClick={() =>
                      setFileTarget({ tool, fileId: f.id, project, tracked: f.tracked, title: `${SCOPE_LABEL[f.scope]} file` })
                    }
                  >
                    {f.editable && !readOnly ? "Open" : "View"}
                  </Button>
                )}
              </td>
            </tr>
          ))}
        </Table>
        {needProject > 0 && (
          <p className="mt-2 text-[11.5px] text-fg-3">
            {needProject} project file{needProject === 1 ? "" : "s"} not shown: choose a project above to see them.
          </p>
        )}
      </Card>

      <Card
        title="Settings"
        icon={SlidersHorizontal}
        sub={`${v.coverage.rows} catalogued settings, ${v.coverage.verified} verified against the vendor's documentation or schema.`}
        stale={view.isStale}
      >
        <div className="mb-3 flex flex-wrap items-end gap-3">
          <Input
            aria-label="Search settings"
            placeholder="Search settings"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            className="w-full sm:w-64"
          />
          <Toggle on={onlySet} onChange={setOnlySet} label="Set only" />
          <Toggle on={onlyCommon} onChange={setOnlyCommon} label="Shared across tools" />
          <Toggle on={onlyDanger} onChange={setOnlyDanger} label="Safety related" />
        </div>
        {rows.length === 0 ? (
          <p className="text-[11.5px] text-fg-3">No settings match.</p>
        ) : (
          <ul className="divide-y divide-line-1" aria-label="Settings">
            {rows.slice(0, limit).map((s) => (
              <li key={s.id} className="flex min-w-0 flex-col gap-1 py-2 sm:flex-row sm:items-start sm:gap-3">
                <div className="min-w-0 flex-1">
                  <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                    <code className="break-all font-mono text-[12px] text-fg-1">{s.path}</code>
                    {s.danger && (
                      <Pill variant="warn" case="none">
                        {s.danger === "refuse_while_integrated" ? "affects capture" : "safety"}
                      </Pill>
                    )}
                    {s.secret && <Pill case="none">secret</Pill>}
                    {s.deprecated && <Pill case="none">deprecated</Pill>}
                    {s.locked && <Pill variant="info" case="none">set by your organization</Pill>}
                    {(s.common ?? []).map((c) => (
                      <Pill key={c} variant="accent" case="none">
                        {c}
                      </Pill>
                    ))}
                  </div>
                  {s.desc && <p className="mt-0.5 line-clamp-2 text-[11.5px] text-fg-3">{s.desc}</p>}
                </div>
                <div className="flex min-w-0 items-start gap-2 sm:w-[300px] sm:shrink-0">
                  <div className="min-w-0 flex-1">
                    <div className="break-all font-mono text-[11.5px] text-fg-1">{effectiveText(s.effective)}</div>
                    <div className="text-[11px] text-fg-4">
                      {s.effective.set && s.effective.layer
                        ? `${layerLabel(s.effective.layer)}${s.effective.file_id ? `, ${s.effective.file_id}` : ""}`
                        : s.default
                          ? `Tool default: ${s.default}`
                          : "Tool default"}
                      {(s.effective.shadowed ?? []).length > 0 && ` (also set in ${(s.effective.shadowed ?? []).join(", ")})`}
                    </div>
                  </div>
                  <Button
                    size="sm"
                    variant="ghost"
                    iconLeft={Pencil}
                    aria-label={`Edit ${s.path}`}
                    onClick={() => setEditing(s)}
                  >
                    {s.writable && !readOnly && !s.read_only_reason ? "Edit" : "Details"}
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
        {rows.length > limit && (
          <div className="mt-2">
            <Button size="sm" variant="ghost" onClick={() => setLimit(limit + SETTINGS_PAGE)}>
              Show more ({rows.length - limit} left)
            </Button>
          </div>
        )}
      </Card>

      <SettingEditor
        view={v}
        setting={editing}
        project={project}
        readOnly={readOnly}
        readOnlyReason={access?.reason}
        access={access}
        confirmToken={confirmToken}
        onClose={() => setEditing(null)}
        onSaved={() => view.reload()}
      />
      <FileEditor
        target={fileTarget}
        confirmToken={confirmToken}
        readOnly={readOnly}
        readOnlyReason={access?.reason}
        onClose={() => setFileTarget(null)}
        onSaved={() => view.reload()}
      />
    </div>
  );
}

export function SettingEditor({
  view,
  setting,
  project,
  readOnly,
  readOnlyReason,
  access,
  confirmToken,
  onClose,
  onSaved,
}: {
  view: ToolView;
  setting: SettingView | null;
  project: string;
  readOnly: boolean;
  readOnlyReason?: string;
  /** The editor's access block: decides whether a locked setting offers an
   *  edit request (T-436). */
  access?: Access | null;
  confirmToken: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  return (
    <SlideOver
      open={setting !== null}
      onClose={onClose}
      title={setting ? <code className="font-mono">{setting.path}</code> : ""}
      subtitle={setting?.id}
      width={720}
      bodyClassName="space-y-4 p-4"
    >
      {setting && (
        <SettingForm
          key={setting.id}
          view={view}
          s={setting}
          project={project}
          readOnly={readOnly}
          readOnlyReason={readOnlyReason}
          access={access}
          confirmToken={confirmToken}
          onSaved={() => {
            onSaved();
            onClose();
          }}
        />
      )}
    </SlideOver>
  );
}

function SettingForm({
  view,
  s,
  project,
  readOnly,
  readOnlyReason,
  access,
  confirmToken,
  onSaved,
}: {
  view: ToolView;
  s: SettingView;
  project: string;
  readOnly: boolean;
  readOnlyReason?: string;
  access?: Access | null;
  confirmToken: string;
  onSaved: () => void;
}) {
  const targets = writableFiles(view, s);
  const [fileId, setFileId] = useState(targets[0]?.id ?? "");
  const current = (s.values ?? []).find((x) => x.file_id === fileId);
  const [text, setText] = useState(current && !current.secret_present ? inputText(s.type, current.value) : "");
  const [remove, setRemove] = useState(false);
  const flow = useSaveFlow(confirmToken, onSaved);
  const parsed = parseInput(s.type, text);
  const blockedReason = readOnly
    ? readOnlyReason
    : s.read_only_reason || (!s.writable ? "Observer does not write this setting." : targets.length === 0 ? "No editable file of this tool takes this setting here." : "");

  const req = {
    tool: view.tool,
    file_id: fileId,
    project: project || undefined,
    edits: [
      remove
        ? { path: keyPath(s.path), op: "delete" as const }
        : { path: keyPath(s.path), op: "set" as const, value: parsed.ok ? parsed.value : undefined },
    ],
  };

  return (
    <>
      {s.desc && <p className="text-[12px] leading-relaxed text-fg-2">{s.desc}</p>}
      <dl className="grid grid-cols-[120px_minmax(0,1fr)] gap-x-3 gap-y-1 text-[11.5px]">
        <dt className="text-fg-4">Type</dt>
        <dd className="text-fg-2">{s.type}</dd>
        {s.default && (
          <>
            <dt className="text-fg-4">Tool default</dt>
            <dd className="break-all font-mono text-fg-2">{s.default}</dd>
          </>
        )}
        <dt className="text-fg-4">Effective now</dt>
        <dd className="break-all font-mono text-fg-1">{effectiveText(s.effective)}</dd>
        <dt className="text-fg-4">Decided by</dt>
        <dd className="text-fg-2">
          {s.effective.set && s.effective.layer ? `${layerLabel(s.effective.layer)} (${s.effective.file_id})` : "Tool default"}
        </dd>
        {s.source && (
          <>
            <dt className="text-fg-4">Source</dt>
            <dd className="break-all text-fg-3">{s.source}</dd>
          </>
        )}
      </dl>
      {(s.values ?? []).length > 0 && (
        <div>
          <div className="mb-1 text-[11px] font-semibold uppercase tracking-[0.06em] text-fg-3">Set in</div>
          <ul className="space-y-0.5 text-[11.5px]">
            {(s.values ?? []).map((x) => (
              <li key={x.file_id} className="flex min-w-0 flex-wrap gap-x-2">
                <span className="text-fg-3">{SCOPE_LABEL[x.scope]}</span>
                <code className="break-all font-mono text-fg-1">
                  {x.secret_present ? "Set (hidden)" : inputText("string", x.value)}
                </code>
              </li>
            ))}
          </ul>
        </div>
      )}
      {blockedReason ? (
        <div className="space-y-1.5 rounded-2 border border-line-2 bg-bg-3 p-2 text-[11.5px] text-fg-3">
          <p>{blockedReason}</p>
          {/* T-436: ask the organization to open this setting for a while. */}
          {canAskToEdit(s, access) && (
            <Link to={configEditRequestLink(s.id)} className="text-accent hover:underline">
              Ask your organization to let you edit this
            </Link>
          )}
        </div>
      ) : (
        <div className="space-y-3">
          {targets.length > 1 && (
            <Select
              label="Write to"
              value={fileId}
              onChange={(e) => {
                setFileId(e.target.value);
                flow.reset();
              }}
              options={targets.map((t) => ({ value: t.id, label: `${SCOPE_LABEL[t.scope]}: ${t.path ?? t.id}` }))}
            />
          )}
          {targets.find((t) => t.id === fileId)?.tracked && <TrackedWarning />}
          <Toggle on={remove} onChange={(v) => { setRemove(v); flow.reset(); }} label="Remove it from this file (fall back to the next layer)" />
          {!remove && <ValueInput s={s} text={text} setText={(t) => { setText(t); flow.reset(); }} error={parsed.ok ? "" : parsed.error} />}
          <SaveFlowFooter
            flow={flow}
            readOnly={false}
            disabled={!remove && !parsed.ok}
            onPreview={() => void flow.runPreview(req)}
            onSave={() => void flow.runSave(req)}
          />
          <SaveFlowBody flow={flow} />
        </div>
      )}
    </>
  );
}

function ValueInput({
  s,
  text,
  setText,
  error,
}: {
  s: SettingView;
  text: string;
  setText: (t: string) => void;
  error: string;
}) {
  // D-571: a string setting picks from the values it already takes (and the
  // models, when it names one); any other value can still be typed.
  const models = useModelOptions(s.type === "string" && isModelSetting(s.path));
  if (s.type === "bool") {
    return (
      <Select
        label="Value"
        value={text}
        onChange={(e) => setText(e.target.value)}
        options={[
          { value: "", label: "Choose" },
          { value: "true", label: "true" },
          { value: "false", label: "false" },
        ]}
        error={error && text !== "" ? error : undefined}
      />
    );
  }
  if ((s.type === "enum" || s.type === "string") && (s.enum ?? []).length > 0) {
    return (
      <Select
        label="Value"
        value={text}
        onChange={(e) => setText(e.target.value)}
        options={[{ value: "", label: "Choose" }, ...(s.enum ?? []).map((x) => ({ value: x, label: x }))]}
      />
    );
  }
  if (s.type === "string") {
    const opts = settingValueOptions(
      [text, s.default, s.effective.value, ...(s.values ?? []).map((v) => v.value)],
      isModelSetting(s.path) ? models.options : [],
    );
    return (
      <div>
        <span className="mb-1 block text-[11px] font-medium text-fg-2">Value</span>
        <SinglePicker
          options={opts}
          value={text}
          onChange={setText}
          allowFree
          loading={models.loading}
          placeholder="Pick a value, or type one…"
          ariaLabel="Value"
        />
        {error && text !== "" && <p className="mt-1 text-[11px] text-danger">{error}</p>}
      </div>
    );
  }
  if (s.type === "int" || s.type === "float" || s.type === "enum") {
    return (
      <Input
        label="Value"
        value={text}
        mono
        onChange={(e) => setText(e.target.value)}
        error={error && text !== "" ? error : undefined}
        help={s.min !== undefined || s.max !== undefined ? `Range ${s.min ?? ""} to ${s.max ?? ""}` : undefined}
      />
    );
  }
  return (
    <Textarea
      label="Value"
      value={text}
      rows={6}
      mono
      onChange={(e) => setText(e.target.value)}
      error={error && text !== "" ? error : undefined}
      help={s.type === "string_list" || s.type === "rule_list" ? "One entry per line, or a JSON array." : "JSON."}
    />
  );
}
