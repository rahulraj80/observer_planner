import clsx from "clsx";

// DiffView renders a unified diff from the server (internal/textdiff) with
// added / removed / hunk lines toned. The server masks credentials before
// it diffs, so this only lays text out. Used by the AI tool settings page
// and the Settings page's config.toml preview.

function lineTone(l: string): string {
  if (l.startsWith("+++") || l.startsWith("---")) return "text-fg-3";
  if (l.startsWith("+")) return "text-success";
  if (l.startsWith("-")) return "text-danger";
  if (l.startsWith("@@")) return "text-info";
  return "text-fg-2";
}

export function DiffView({ diff, maxHeight = 320 }: { diff: string; maxHeight?: number }) {
  if (!diff) return null;
  const lines = diff.replace(/\n$/, "").split("\n");
  return (
    <pre
      aria-label="Unified diff"
      className="overflow-auto rounded-2 border border-line-2 bg-bg-3 p-2 font-mono text-[11px] leading-[1.5]"
      style={{ maxHeight }}
    >
      {lines.map((l, i) => (
        <div key={i} className={clsx("whitespace-pre", lineTone(l))}>
          {l || " "}
        </div>
      ))}
    </pre>
  );
}
