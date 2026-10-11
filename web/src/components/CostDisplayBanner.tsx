import { costDisplayNote, useCostDisplay } from "@/lib/costDisplay";

// CostDisplayBanner discloses that the developer's org chose token usage
// instead of dollar costs for developer dashboards. Not dismissable: like
// DemoBanner it is state disclosure, and it is the reason a cost figure on
// this page reads "212k tok" or is hidden. Renders nothing in dollars mode.
export function CostDisplayBanner() {
  const info = useCostDisplay();
  const note = costDisplayNote(info);
  if (!note) return null;
  return (
    <div
      role="status"
      className="flex items-center gap-2 border-b border-line-2 bg-bg-2 px-4 py-1.5 text-[11.5px] text-fg-2"
    >
      <span className="shrink-0 font-semibold text-fg-1">Token usage view</span>
      <span className="min-w-0 truncate">{note}</span>
    </div>
  );
}
