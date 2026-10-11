import { useState } from "react";
import { Eye, Layers, Pencil, Save } from "lucide-react";
import {
  Button,
  Card,
  ErrorState,
  InlineLoading,
  Input,
  Pill,
  Select,
  SinglePicker,
  SlideOver,
  Textarea,
  Toggle,
} from "@/components/primitives";
import { isModelSetting, settingValueOptions } from "@/lib/pickerOptions";
import { useModelOptions } from "@/lib/usePickerSources";
import { pushToast } from "@/components/Toast";
import { useApi } from "@/lib/useApi";
import {
  layerLabel,
  blocking,
  countNoun,
  formatValue,
  inputText,
  parseInput,
  type Access,
  type CommonKeyView,
  type CommonPreview,
  type CommonResponse,
  type CommonPlacement,
  commonScope,
} from "@/lib/agentConfig";
import { asCallError, previewCommon, saveCommon } from "./api";
import { DiffView, FindingsList } from "./PreviewPanel";

// CommonTab: the common vocabulary (plan section 4). One value set here is
// written into every INSTALLED tool that exposes the key, each in its own
// native shape, all or nothing. Tools without the key are named, never
// silently skipped.

const TIER_LABEL: Record<string, string> = {
  core: "Core",
  secondary: "Secondary",
  observer_owned: "Observer managed",
};

const STATUS_LABEL: Record<string, string> = {
  write: "Will write",
  not_installed: "Not installed",
  refused: "Refused",
  not_writable: "Not writable",
  no_user_file: "No user file",
  no_project_file: "No project file",
};

export function CommonTab({
  project,
  access,
  confirmToken,
}: {
  project: string;
  access: Access | null;
  confirmToken: string;
}) {
  const common = useApi<CommonResponse>("/api/agent-config/common", { project: project || undefined }, [project]);
  const [editing, setEditing] = useState<CommonKeyView | null>(null);
  const readOnly = access?.read_only ?? false;

  if (common.error && !common.data) return <ErrorState error={common.error} onRetry={common.reload} />;
  if (!common.data) return <InlineLoading label="Reading every tool's settings" />;

  return (
    <div className="space-y-4">
      <Card
        title="One value, every AI tool"
        icon={Layers}
        sub={
          project
            ? "Each key maps onto every tool that has it. A value set here is written into each installed tool's project file in the selected project (the project level)."
            : "Each key maps onto every tool that has it. A value set here is written into each installed tool's own user settings file."
        }
        stale={common.isStale}
      >
        <ul className="divide-y divide-line-1" aria-label="Shared settings">
          {common.data.keys.map((k) => (
            <li key={k.spec.key} className="flex min-w-0 flex-col gap-2 py-2.5 sm:flex-row sm:items-start sm:gap-3">
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-1.5">
                  <code className="font-mono text-[12px] text-fg-1">{k.spec.key}</code>
                  <Pill case="none">{TIER_LABEL[k.spec.tier] ?? k.spec.tier}</Pill>
                  {(k.spec.danger ?? []).length > 0 && (
                    <Pill variant="warn" case="none">
                      safety
                    </Pill>
                  )}
                </div>
                <p className="mt-0.5 line-clamp-2 text-[11.5px] text-fg-3">{k.spec.desc}</p>
                <p className="mt-1 text-[11px] text-fg-4">
                  {countNoun(k.exposed, "tool")} {k.exposed === 1 ? "has" : "have"} it{k.lossy > 0 ? `, ${k.lossy} approximately` : ""}; set in {countNoun(k.set_in, "tool")}.
                </p>
              </div>
              <div className="sm:shrink-0">
                <Button size="sm" variant="ghost" iconLeft={Pencil} onClick={() => setEditing(k)}>
                  {readOnly || k.spec.tier === "observer_owned" ? "Details" : "Set"}
                </Button>
              </div>
            </li>
          ))}
        </ul>
      </Card>
      <SlideOver
        open={editing !== null}
        onClose={() => setEditing(null)}
        title={editing ? <code className="font-mono">{editing.spec.key}</code> : ""}
        subtitle={project ? "Set in every installed AI tool, for this project" : "Set in every installed AI tool"}
        width={760}
        bodyClassName="space-y-4 p-4"
      >
        {editing && (
          <CommonForm
            key={editing.spec.key}
            k={editing}
            readOnly={readOnly}
            readOnlyReason={access?.reason}
            project={project}
            confirmToken={confirmToken}
            onSaved={() => {
              common.reload();
              setEditing(null);
            }}
          />
        )}
      </SlideOver>
    </div>
  );
}

function CommonForm({
  k,
  readOnly,
  readOnlyReason,
  project,
  confirmToken,
  onSaved,
}: {
  k: CommonKeyView;
  readOnly: boolean;
  readOnlyReason?: string;
  project: string;
  confirmToken: string;
  onSaved: () => void;
}) {
  const spec = k.spec;
  const first = k.tools.find((t) => t.set && !t.masked);
  const [text, setText] = useState(first ? inputText(spec.type, first.value) : "");
  // D-571: a string key picks from the values the tools already carry (and
  // the models, when the key names one); any other value can still be typed.
  const models = useModelOptions(spec.type === "string" && isModelSetting(spec.key));
  const [clear, setClear] = useState(false);
  const [placement, setPlacement] = useState<CommonPlacement>("local_only");
  const scope = commonScope(project, placement);
  const [preview, setPreview] = useState<CommonPreview | null>(null);
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState<"" | "preview" | "save">("");
  const [error, setError] = useState("");
  const parsed = parseInput(spec.type, text);
  const ownedElsewhere = spec.tier === "observer_owned";
  const req = { key: spec.key, clear, value: clear || !parsed.ok ? undefined : parsed.value, ...scope.fields };

  function reset() {
    setPreview(null);
    setConfirmed(false);
    setError("");
  }

  async function runPreview() {
    setBusy("preview");
    reset();
    try {
      setPreview(await previewCommon(req));
    } catch (e) {
      setError(asCallError(e).message);
    } finally {
      setBusy("");
    }
  }

  async function runSave() {
    if (!preview) return;
    setBusy("save");
    setError("");
    try {
      const out = await saveCommon({ ...req, preview_etag: preview.preview_etag, confirm: confirmed }, confirmToken);
      pushToast(`Saved in ${countNoun((out.written ?? []).length, "file")}`, "success");
      onSaved();
    } catch (e) {
      const ce = asCallError(e);
      const p = ce.body?.preview;
      if (p && "targets" in p) setPreview(p);
      setError(ce.message);
    } finally {
      setBusy("");
    }
  }

  const anyBlocked = (preview?.targets ?? []).some((t) => t.preview && blocking(t.preview.findings).length > 0);
  const canSave =
    !readOnly && preview !== null && preview.writes > 0 && !anyBlocked && (preview.ok || (preview.needs_confirm && confirmed));

  return (
    <>
      <p className="text-[12px] leading-relaxed text-fg-2">{spec.desc}</p>
      <div>
        <div className="mb-1 text-[11px] font-semibold uppercase tracking-[0.06em] text-fg-3">Per tool now</div>
        <ul className="space-y-1 text-[11.5px]">
          {k.tools.map((t) => (
            <li key={t.tool} className="flex min-w-0 flex-wrap items-baseline gap-x-2">
              <span className="text-fg-1">{t.tool}</span>
              <code className="min-w-0 break-all font-mono text-fg-2">
                {t.error ? t.error : t.set ? (t.masked ? "Set (hidden)" : formatValue(t.value)) : "Not set"}
              </code>
              {t.layer && t.set && <span className="text-fg-4">{layerLabel(t.layer)}</span>}
              {t.lossy && <Pill case="none">approximate</Pill>}
            </li>
          ))}
        </ul>
        {(k.not_exposed ?? []).length > 0 && (
          <p className="mt-1 text-[11px] text-fg-4">Not available in: {(k.not_exposed ?? []).join(", ")}.</p>
        )}
      </div>
      {readOnly || ownedElsewhere ? (
        <p className="rounded-2 border border-line-2 bg-bg-3 p-2 text-[11.5px] text-fg-3">
          {ownedElsewhere ? "Observer manages this key through its own setup (init and the proxy route), not here." : readOnlyReason}
        </p>
      ) : (
        <div className="space-y-3">
          <Toggle
            on={clear}
            onChange={(v) => {
              setClear(v);
              reset();
            }}
            label={project ? "Remove it from every installed tool's project file here" : "Remove it from every installed tool's user settings"}
          />
          {project && (
            <Select
              label="Project file"
              value={placement}
              onChange={(e) => {
                setPlacement(e.target.value as CommonPlacement);
                reset();
              }}
              options={[
                { value: "local_only", label: "Personal file (kept out of git)" },
                { value: "shared", label: "Shared file (commits with the repo)" },
              ]}
            />
          )}
          <p className={scope.tracked ? "text-[11.5px] text-warn" : "text-[11.5px] text-fg-3"}>Writes into {scope.where}.</p>
          {!clear &&
            ((spec.enum ?? []).length > 0 ? (
              <Select
                label="Value"
                value={text}
                onChange={(e) => {
                  setText(e.target.value);
                  reset();
                }}
                options={[{ value: "", label: "Choose" }, ...(spec.enum ?? []).map((x) => ({ value: x, label: x }))]}
              />
            ) : spec.type === "bool" ? (
              <Select
                label="Value"
                value={text}
                onChange={(e) => {
                  setText(e.target.value);
                  reset();
                }}
                options={[
                  { value: "", label: "Choose" },
                  { value: "true", label: "true" },
                  { value: "false", label: "false" },
                ]}
              />
            ) : spec.type === "string" ? (
              <div>
                <span className="mb-1 block text-[11px] font-medium text-fg-2">Value</span>
                <SinglePicker
                  options={settingValueOptions(
                    [text, ...k.tools.filter((t) => !t.masked).map((t) => t.value)],
                    isModelSetting(spec.key) ? models.options : [],
                  )}
                  value={text}
                  onChange={(v) => {
                    setText(v);
                    reset();
                  }}
                  allowFree
                  loading={models.loading}
                  placeholder="Pick a value, or type one…"
                  ariaLabel="Value"
                />
                {!parsed.ok && text !== "" && <p className="mt-1 text-[11px] text-danger">{parsed.error}</p>}
              </div>
            ) : spec.type === "int" || spec.type === "float" ? (
              <Input
                label="Value"
                mono
                value={text}
                onChange={(e) => {
                  setText(e.target.value);
                  reset();
                }}
                error={!parsed.ok && text !== "" ? parsed.error : undefined}
              />
            ) : (
              <Textarea
                label="Value"
                mono
                rows={6}
                value={text}
                onChange={(e) => {
                  setText(e.target.value);
                  reset();
                }}
                error={!parsed.ok && text !== "" ? parsed.error : undefined}
              />
            ))}
          <div className="flex flex-wrap items-center gap-2">
            {preview?.needs_confirm && !anyBlocked && (
              <Toggle on={confirmed} onChange={setConfirmed} label="I understand this loosens the tools' safety" />
            )}
            <div className="flex flex-wrap gap-2">
              <Button
                size="sm"
                iconLeft={Eye}
                loading={busy === "preview"}
                disabled={busy !== "" || (!clear && !parsed.ok)}
                onClick={() => void runPreview()}
              >
                Check and preview
              </Button>
              <Button
                size="sm"
                variant="primary"
                iconLeft={Save}
                loading={busy === "save"}
                disabled={!canSave || busy !== ""}
                onClick={() => void runSave()}
              >
                Save everywhere
              </Button>
            </div>
          </div>
          {error && (
            <p role="alert" className="rounded-2 border border-danger bg-danger-soft p-2 text-[11.5px] text-danger">
              {error}
            </p>
          )}
          {preview && (
            <ul className="space-y-3" aria-label="Per tool preview">
              {preview.targets.map((t) => (
                <li key={t.tool} className="rounded-2 border border-line-1 p-2">
                  <div className="mb-1 flex flex-wrap items-center gap-2 text-[11.5px]">
                    <span className="font-medium text-fg-1">{t.tool}</span>
                    <Pill variant={t.status === "write" ? "success" : t.status === "refused" ? "danger" : "neutral"} case="none">
                      {STATUS_LABEL[t.status] ?? t.status}
                    </Pill>
                    {t.lossy && <Pill case="none">approximate</Pill>}
                    {t.path && <code className="min-w-0 break-all font-mono text-[11px] text-fg-3">{t.path}</code>}
                  </div>
                  {t.reason && <p className="text-[11.5px] text-fg-3">{t.reason}</p>}
                  {t.preview && (
                    <div className="space-y-2">
                      <FindingsList findings={t.preview.findings} />
                      <DiffView diff={t.preview.diff} />
                    </div>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </>
  );
}
