import { useCallback, useEffect, useRef, useState } from "react";
import { FileDiff } from "lucide-react";
import { Button, Modal } from "@/components/primitives";
import { ConfigSavePreview } from "@/components/ConfigSavePreview";
import { registerConfigSavePreview, type ConfigSavePreviewRequest } from "@/lib/configSectionSave";

// ConfigSavePreviewHost is the one dialog every whole-section config save
// opens before it writes (T-432; lib/configSectionSave.ts). Mounted once in
// the shell. It shows the server's dry run (the masked diff, credential
// warnings, restart and confirm notes) and answers Save or Cancel; requests
// that arrive while one is open wait their turn.

type Pending = ConfigSavePreviewRequest & { resolve: (ok: boolean) => void };

export function ConfigSavePreviewHost() {
  const [queue, setQueue] = useState<Pending[]>([]);
  const queueRef = useRef(queue);
  queueRef.current = queue;

  useEffect(
    () =>
      registerConfigSavePreview(
        (req) =>
          new Promise<boolean>((resolve) => {
            setQueue((q) => [...q, { ...req, resolve }]);
          }),
      ),
    [],
  );

  // A host that unmounts with prompts open answers them "cancel" so no
  // caller waits forever.
  useEffect(
    () => () => {
      for (const p of queueRef.current) p.resolve(false);
    },
    [],
  );

  const current = queue[0];
  const answer = useCallback((ok: boolean) => {
    setQueue((q) => {
      q[0]?.resolve(ok);
      return q.slice(1);
    });
  }, []);

  const warnings = current?.dryRun.secret_warnings.length ?? 0;
  return (
    <Modal
      open={current != null}
      onClose={() => answer(false)}
      icon={FileDiff}
      title={current?.title ? `Review change: ${current.title}` : "Review config.toml change"}
      subtitle={
        current ? (
          <>
            Section <span className="font-mono">[{current.section}]</span> in{" "}
            <span className="font-mono break-all">{current.dryRun.config_path}</span>. Nothing is written until you save.
          </>
        ) : null
      }
      width={680}
      footer={
        <>
          <Button variant="ghost" onClick={() => answer(false)} data-testid="config-save-cancel">
            Cancel
          </Button>
          <Button variant="primary" onClick={() => answer(true)} data-testid="config-save-confirm">
            {warnings > 0 ? "Save anyway" : "Save"}
          </Button>
        </>
      }
    >
      {current && <ConfigSavePreview data={current.dryRun} />}
    </Modal>
  );
}
