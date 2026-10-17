import { Pill } from "@/components/primitives";
import { DiffView } from "@/components/DiffView";
import {
  SEVERITY_LABEL,
  formatValue,
  severityTone,
  sortFindings,
  type Change,
  type Finding,
  type Preview,
} from "@/lib/agentConfig";

// PreviewPanel renders the sanity check's verdict on one proposed save:
// the findings (blocking first), the per-key changes and the unified diff.
// Secret values arrive from the server already hidden, so nothing here can
// show one.

export { DiffView };

export function FindingsList({ findings }: { findings: Finding[] | null | undefined }) {
  const rows = sortFindings(findings);
  if (rows.length === 0) {
    return <p className="text-[11.5px] text-fg-3">The sanity check found nothing to flag.</p>;
  }
  return (
    <ul className="space-y-1.5" aria-label="Sanity check findings">
      {rows.map((f, i) => (
        <li key={`${f.code}-${f.path ?? ""}-${i}`} className="flex min-w-0 items-start gap-2 text-[11.5px]">
          <Pill variant={severityTone(f.severity)} case="none" className="shrink-0">
            {SEVERITY_LABEL[f.severity]}
          </Pill>
          <span className="min-w-0 text-fg-2">
            {f.path && <code className="mr-1 break-all font-mono text-fg-1">{f.path}</code>}
            {f.message}
            {f.line ? <span className="ml-1 text-fg-4">(line {f.line})</span> : null}
          </span>
        </li>
      ))}
    </ul>
  );
}

function changeText(c: Change): string {
  if (c.secret) return c.op === "delete" ? "hidden value removed" : "hidden value";
  if (c.op === "delete") return `removed (was ${formatValue(c.before)})`;
  if (c.op === "add") return formatValue(c.after);
  return `${formatValue(c.before)} -> ${formatValue(c.after)}`;
}

export function ChangesList({ changes }: { changes: Change[] | null | undefined }) {
  const rows = changes ?? [];
  if (rows.length === 0) return null;
  return (
    <ul className="space-y-1" aria-label="Changed keys">
      {rows.map((c) => (
        <li key={c.path} className="flex min-w-0 flex-wrap items-baseline gap-x-2 text-[11.5px]">
          <code className="break-all font-mono text-fg-1">{c.path}</code>
          <span className="min-w-0 break-all text-fg-3">{changeText(c)}</span>
          {c.danger && (
            <Pill variant="warn" case="none">
              {c.danger === "refuse_while_integrated" ? "affects capture" : "loosens safety"}
            </Pill>
          )}
          {c.uncatalogued && <Pill case="none">not in catalogue</Pill>}
          {c.shadowed_by && <Pill case="none">overridden by {c.shadowed_by}</Pill>}
        </li>
      ))}
    </ul>
  );
}

export function PreviewPanel({ preview }: { preview: Preview }) {
  return (
    <div className="space-y-3" data-testid="agent-config-preview">
      <div className="flex flex-wrap items-center gap-2 text-[11.5px]">
        {preview.no_change ? (
          <Pill case="none">No change</Pill>
        ) : preview.ok ? (
          <Pill variant="success" case="none">
            Ready to save
          </Pill>
        ) : preview.needs_confirm ? (
          <Pill variant="warn" case="none">
            Needs your confirm
          </Pill>
        ) : (
          <Pill variant="danger" case="none">
            Refused
          </Pill>
        )}
        {preview.created && <Pill case="none">creates the file</Pill>}
        {!preview.no_change && (
          <span className="text-fg-3">
            +{preview.diff_stats.added} / -{preview.diff_stats.removed} lines
          </span>
        )}
        <code className="min-w-0 break-all font-mono text-[11px] text-fg-3">{preview.path}</code>
      </div>
      <FindingsList findings={preview.findings} />
      <ChangesList changes={preview.changes} />
      {(preview.disclosures ?? []).length > 0 && (
        <ul className="space-y-1 text-[11.5px] text-warn">
          {(preview.disclosures ?? []).map((d, i) => (
            <li key={i}>
              {d.path.join(".")}: {d.reason}
            </li>
          ))}
        </ul>
      )}
      {preview.add_exclude && (
        <p className="text-[11.5px] text-fg-3">
          This creates a project-local file, so Observer also adds it to the repository's .git/info/exclude.
        </p>
      )}
      <DiffView diff={preview.diff} />
    </div>
  );
}

/** TrackedWarning is the repo-commit note on a file git tracks (plan section 6.3). */
export function TrackedWarning() {
  return (
    <p className="rounded-2 border border-line-2 bg-warn-soft p-2 text-[11.5px] text-fg-2">
      This is a shared project file: a change here shows up in git status and can be committed with the repository.
    </p>
  );
}
