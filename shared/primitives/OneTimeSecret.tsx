import { useState } from "react";
import type { ReactNode } from "react";
import clsx from "clsx";
import { Check, Copy, KeyRound } from "lucide-react";
import { Button } from "./Button";
import { Icon } from "./Icon";

// OneTimeSecret — the shown-once secret callout: a value the server returns
// exactly once (a client secret, an API key's plaintext, an enrolment token)
// with a Copy button, the "you won't see this again" warning, and an
// acknowledgement that dismisses it. One component for every such reveal
// (Agent Access UI audit 2026-10-01: the credential mint and the API-key mint
// had two hand-rolled boxes, one without a copy button).
//
// Pure: the caller owns whether it is shown (render it while the value is in
// state; `onDismiss` clears that state). The only side effect is the
// clipboard write on Copy, the same one CopyOnClick performs.
export function OneTimeSecret({
  value,
  title = "Copy this secret now",
  description,
  warning = "You won't see it again. Store it somewhere safe before you leave this page.",
  onDismiss,
  dismissLabel = "I have stored it",
  copyLabel = "Copy",
  className,
}: {
  /** The plaintext secret. Rendered in full, monospace, wrapping. */
  value: string;
  title?: ReactNode;
  /** Optional context: what the secret is and where it goes. */
  description?: ReactNode;
  /** The shown-once warning line (default says it will not be shown again). */
  warning?: ReactNode;
  /** Renders the acknowledgement button; omit to keep the callout until the
   *  caller removes it. */
  onDismiss?: () => void;
  dismissLabel?: string;
  copyLabel?: string;
  className?: string;
}) {
  const [copied, setCopied] = useState(false);
  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard denied (insecure context, permissions): the value stays
      // selectable on screen, so the operator can still copy it by hand.
      setCopied(false);
    }
  }
  return (
    <div
      role="alert"
      className={clsx("rounded-3 border border-warn/50 bg-warn-soft px-3 py-2.5", className)}
    >
      <div className="flex items-start gap-2">
        <Icon icon={KeyRound} size="sm" className="mt-0.5 shrink-0 text-warn" />
        <div className="min-w-0 flex-1 space-y-1.5">
          <p className="text-[12.5px] font-semibold text-fg-1">{title}</p>
          {description && <p className="text-[11.5px] leading-snug text-fg-2">{description}</p>}
          <code className="block select-all break-all rounded-2 border border-line-2 bg-bg-2 px-2 py-1.5 font-mono text-[12px] text-fg-0">
            {value}
          </code>
          <p className="text-[11px] leading-snug text-warn">{warning}</p>
          <div className="flex flex-wrap items-center gap-2 pt-0.5">
            <Button variant="soft" size="sm" iconLeft={copied ? Check : Copy} onClick={() => void copy()}>
              {copied ? "Copied" : copyLabel}
            </Button>
            {onDismiss && (
              <Button variant="ghost" size="sm" onClick={onDismiss}>
                {dismissLabel}
              </Button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
