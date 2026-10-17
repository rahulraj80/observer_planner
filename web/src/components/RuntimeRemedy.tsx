import { CopyOnClick } from "@/components/CopyOnClick";
import type { RuntimeRemedyParts } from "@/lib/runtimeCheck";

// RuntimeRemedy renders one too-old runtime check in readable parts (the
// server's internal/toolinstall CheckRuntime split): the short sentence, the
// checked interpreter's path in small text, the fix command on its own in the
// same monospace box the install command uses (with a Copy button), and one
// note. showSummary is false where the caller already shows the sentence as
// its headline.
export function RuntimeRemedy({ parts, showSummary = true }: { parts: RuntimeRemedyParts; showSummary?: boolean }) {
  return (
    <div data-testid="runtime-remedy">
      {showSummary && <div>{parts.summary}</div>}
      {parts.path && <div className="mt-0.5 break-all text-[10px] text-fg-3">Checked {parts.path}</div>}
      {parts.command && (
        <>
          <div className="mt-1.5 text-fg-3">Install a newer {parts.display} first:</div>
          <div className="mt-1 flex items-start gap-1.5">
            <code className="block min-w-0 flex-1 break-all rounded-2 bg-bg-0 px-2 py-1 font-mono text-[10.5px] text-fg-1">
              {parts.command}
            </code>
            <CopyOnClick
              value={parts.command}
              title="Copy the command"
              ariaLabel={`Copy the ${parts.display} install command`}
              className="shrink-0 rounded-2 border border-line-2 bg-bg-2 px-2 py-1 text-caption text-fg-2 hover:bg-bg-3"
            >
              Copy
            </CopyOnClick>
          </div>
        </>
      )}
      {parts.note && <div className="mt-1 text-[10.5px] text-fg-3">{parts.note}</div>}
    </div>
  );
}
