import { Pill } from "@/components/primitives";
import { DiffView } from "@/components/DiffView";
import type { ConfigKeysDryRun } from "@/lib/configSchema";

// ConfigSavePreview shows the server's dry run of a config.toml save: what
// would change, how it would be written, when each key takes effect, and any
// value that looks like a credential. One component for both save routes
// (the Settings page's key editor and every whole-section save, T-432).
export function ConfigSavePreview({ data }: { data: ConfigKeysDryRun }) {
  return (
    <div className="space-y-2 rounded-2 border border-line-2 bg-bg-3/40 p-3 text-[11.5px]" data-testid="config-preview">
      <div className="flex flex-wrap items-center gap-2">
        <Pill variant={data.would_write ? "success" : "neutral"} case="none">
          {data.would_write ? "Ready to save" : "No change"}
        </Pill>
        {data.needs_confirm && (
          <Pill variant="warn" case="none">
            sensitive: confirm on save
          </Pill>
        )}
        <span className="text-fg-3">
          +{data.diff_stats.added} / -{data.diff_stats.removed} lines,{" "}
          {data.write_mode === "surgical" ? "comments kept" : "file re-serialized (comments not kept)"}
        </span>
      </div>
      {data.restart_required_keys.length > 0 && (
        <p className="text-fg-3">
          Applies on restart: <span className="font-mono">{data.restart_required_keys.join(", ")}</span>
        </p>
      )}
      {data.secret_warnings.map((w) => (
        <p key={w.key} className="text-warn" data-testid="config-preview-secret-warning">
          <span className="font-mono">{w.key}</span>: {w.message}
        </p>
      ))}
      <DiffView diff={data.diff} maxHeight={260} />
    </div>
  );
}
