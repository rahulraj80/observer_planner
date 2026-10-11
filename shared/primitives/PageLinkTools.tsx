import { useState } from "react";
import { Check, ExternalLink, Link } from "lucide-react";
import { Icon } from "./Icon";
import { Tooltip } from "./Tooltip";

// PageLinkTools are the top bar's "Open in new tab" and "Copy link" buttons:
// the page's own URL, which carries its window, filters, tab and open drawer,
// so a copied link reopens exactly this view. Promoted from the node
// dashboard's TopBar so the org dashboard's top bar has them too (T-451).
// The copy button confirms with a check for a moment; a browser that refuses
// the clipboard says so in the tooltip instead of failing silently.
export function PageLinkTools({ className }: { className?: string }) {
  const [copied, setCopied] = useState<"idle" | "ok" | "failed">("idle");
  const copy = async () => {
    try {
      if (!navigator.clipboard) throw new Error("no clipboard");
      await navigator.clipboard.writeText(window.location.href);
      setCopied("ok");
    } catch {
      setCopied("failed");
    }
    window.setTimeout(() => setCopied("idle"), 1600);
  };
  const copyTip = copied === "ok" ? "Link copied" : copied === "failed" ? "This browser blocked the clipboard" : "Copy link";
  return (
    <span className={className}>
      <span className="flex items-center gap-2">
        <Tooltip content="Open in new tab">
          <button
            type="button"
            aria-label="Open in new tab"
            onClick={() => window.open(window.location.href, "_blank", "noopener")}
            className="grid h-7 w-7 place-items-center rounded-2 border border-line-2 bg-bg-2 text-fg-3 hover:bg-bg-3 hover:text-fg-0"
          >
            <Icon icon={ExternalLink} size="xs" />
          </button>
        </Tooltip>
        <Tooltip content={copyTip}>
          <button
            type="button"
            aria-label={copyTip}
            onClick={() => void copy()}
            className="grid h-7 w-7 place-items-center rounded-2 border border-line-2 bg-bg-2 text-fg-3 hover:bg-bg-3 hover:text-fg-0"
          >
            <Icon icon={copied === "ok" ? Check : Link} size="xs" />
          </button>
        </Tooltip>
      </span>
    </span>
  );
}
