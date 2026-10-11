import { useState } from "react";
import { Eye, Save } from "lucide-react";
import { Button, Toggle } from "@/components/primitives";
import { pushToast } from "@/components/Toast";
import { blocking, type Preview, type SaveOutcome, type SaveRequest } from "@/lib/agentConfig";
import { asCallError, previewSave, save } from "./api";
import { PreviewPanel } from "./PreviewPanel";

// useSaveFlow is the one preview -> confirm -> save sequence every editor on
// the page uses (a setting form, a raw file, an instruction file). The
// server runs the whole sanity check on both calls; the save carries the
// preview's etag, so a file that changed in between is refused as stale and
// the fresh preview is shown instead.

export type SaveFlow = {
  preview: Preview | null;
  error: string;
  busy: "" | "preview" | "save";
  confirmed: boolean;
  setConfirmed: (v: boolean) => void;
  runPreview: (req: SaveRequest) => Promise<void>;
  runSave: (req: SaveRequest) => Promise<SaveOutcome | null>;
  reset: () => void;
};

export function useSaveFlow(confirmToken: string, onSaved?: (o: SaveOutcome) => void): SaveFlow {
  const [preview, setPreview] = useState<Preview | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<"" | "preview" | "save">("");
  const [confirmed, setConfirmed] = useState(false);

  async function runPreview(req: SaveRequest) {
    setBusy("preview");
    setError("");
    setConfirmed(false);
    try {
      setPreview(await previewSave(req));
    } catch (e) {
      const ce = asCallError(e);
      setPreview(null);
      setError(ce.message);
    } finally {
      setBusy("");
    }
  }

  async function runSave(req: SaveRequest): Promise<SaveOutcome | null> {
    if (!preview) return null;
    setBusy("save");
    setError("");
    try {
      const out = await save({ ...req, preview_etag: preview.preview_etag, confirm: confirmed }, confirmToken);
      if (out.no_change) {
        pushToast("Nothing to save: the file already says this.", "info");
      } else {
        pushToast(out.created ? `Created ${out.path}` : `Saved ${out.path}`, "success");
      }
      setPreview(null);
      setConfirmed(false);
      onSaved?.(out);
      return out;
    } catch (e) {
      const ce = asCallError(e);
      const p = ce.body?.preview;
      if (p && "diff" in p) setPreview(p);
      setError(ce.message);
      return null;
    } finally {
      setBusy("");
    }
  }

  return {
    preview,
    error,
    busy,
    confirmed,
    setConfirmed,
    runPreview,
    runSave,
    reset: () => {
      setPreview(null);
      setError("");
      setConfirmed(false);
    },
  };
}

/** SaveFlowFooter is the preview + save button row with the confirm switch. */
export function SaveFlowFooter({
  flow,
  readOnly,
  readOnlyReason,
  onPreview,
  onSave,
  disabled,
}: {
  flow: SaveFlow;
  readOnly: boolean;
  readOnlyReason?: string;
  onPreview: () => void;
  onSave: () => void;
  disabled?: boolean;
}) {
  const p = flow.preview;
  const blocked = p ? blocking(p.findings).length > 0 : false;
  const canSave =
    !readOnly && p !== null && !p.no_change && !blocked && (p.ok || (p.needs_confirm && flow.confirmed));
  // Buttons lead (left): the app's floating launcher sits bottom-right and
  // must never cover Save.
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Button
        variant="secondary"
        size="sm"
        iconLeft={Eye}
        loading={flow.busy === "preview"}
        disabled={disabled || flow.busy !== ""}
        onClick={onPreview}
      >
        Check and preview
      </Button>
      <Button
        variant="primary"
        size="sm"
        iconLeft={Save}
        loading={flow.busy === "save"}
        disabled={!canSave || flow.busy !== ""}
        onClick={onSave}
        title={readOnly ? readOnlyReason : undefined}
      >
        Save
      </Button>
      {p?.needs_confirm && !blocked && (
        <Toggle
          on={flow.confirmed}
          onChange={flow.setConfirmed}
          label="I understand this loosens the tool's safety"
          disabled={readOnly}
        />
      )}
    </div>
  );
}

/** SaveFlowBody shows the error and the current preview. */
export function SaveFlowBody({ flow }: { flow: SaveFlow }) {
  return (
    <>
      {flow.error && (
        <p role="alert" className="rounded-2 border border-danger bg-danger-soft p-2 text-[11.5px] text-danger">
          {flow.error}
        </p>
      )}
      {flow.preview && <PreviewPanel preview={flow.preview} />}
    </>
  );
}
