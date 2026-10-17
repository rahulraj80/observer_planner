import { useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { Check, Copy, Inbox, type LucideIcon } from "lucide-react";
import clsx from "clsx";
import { Icon as Glyph } from "./Icon";
import { useHelpSlot } from "./helpSlot";
import { EmptyIllustration, type IllustrationKind } from "./EmptyIllustration";
import { InfoDisclosure } from "./InfoDisclosure";

// EmptyState — the shared teaching empty state for every app (promoted from
// web2 2026-09-28; web2's old path is a re-export shim). Originally G11: it replaced
// the grab-bag of bespoke per-page "No data." lines and one-off NotConfigured
// cards with one component that TEACHES: what the surface shows, the exact
// dependency to configure to get data, and a way to reach the deeper help
// (help drawer via `helpId`, an in-app link via `to`, and/or a docs pointer).
//
// Intentionally token-styled to match Card/StatCard so it reads as part of the
// system in both light and dark. Keep the copy honest — name the real missing
// dependency (a node opt-in, or raised by the org on a managed node; a poller
// key; a proxy route), never imply "coming soon".
export type EmptyStateProps = {
  // Short headline — the affirmative "what would be here".
  title: string;
  // One or two sentences: what this surface shows + what to configure.
  body?: ReactNode;
  // Optional numbered/plain teaching steps.
  steps?: ReactNode[];
  icon?: LucideIcon;
  // Empty-state art (shared/primitives/EmptyIllustration). When set it
  // replaces the icon tile; kinds: sessions, chart, search, all-clear, locked,
  // offline, connect, inbox, error, enrich, cohort, setup.
  illustration?: IllustrationKind;
  // Illustration width in px (default 160 card / 120 inline).
  illustrationSize?: number;
  // Opens the help drawer scrolled to this registry entry (see help.ts).
  helpId?: string;
  // In-app call-to-action (e.g. "/invite" to enroll an agent).
  to?: string;
  toLabel?: string;
  // Docs pointer rendered as a monospace hint.
  // @deprecated repo paths aren't reachable from the app — use `guideId` for
  // a real in-app help-drawer deep link instead.
  docHint?: string;
  // Copyable config snippet (e.g. a `[org_client.share...]` config.toml
  // block). It sits CLOSED behind a "Learn more" disclosure (design review
  // 2026-10-13): the body carries the plain instruction, the config key is
  // the detail for whoever sets up one machine by hand.
  snippet?: string;
  // The disclosure's summary, e.g. "Set it up on one machine". Defaults to
  // "Learn more".
  snippetLabel?: string;
  // Opens the help drawer scrolled to this guide entry ("Read the guide").
  guideId?: string;
  // Renders a "Notify node operators" link to the Announcements page.
  notifyOperators?: boolean;
  // Extra content (e.g. a per-vendor list) rendered under the body.
  children?: ReactNode;
  // "card" (default) draws the bordered panel; "inline" is a compact
  // borderless variant for inside an existing Card/ChartShell.
  variant?: "card" | "inline";
  className?: string;
};

export function EmptyState({
  title,
  body,
  steps,
  icon: Icon = Inbox,
  illustration,
  illustrationSize,
  helpId,
  to,
  toLabel = "Get started",
  docHint,
  snippet,
  snippetLabel,
  guideId,
  notifyOperators,
  children,
  variant = "card",
  className,
}: EmptyStateProps) {
  const renderHelp = useHelpSlot();
  // ONE shape (design review 2026-10-13, docs/app-design-system.md "Empty
  // states"): the art or the icon tile centred above a centred title, a line
  // of body and the call to action, in both variants. The card variant
  // without an illustration used to put a small icon LEFT of left-aligned
  // copy, a second shape beside the centred one. Steps, a snippet and
  // children stay left-aligned inside a centred block so a list still reads.
  return (
    <div
      className={clsx(
        "sb-scale-in text-center",
        variant === "card" ? "rounded-3 border border-line-2 bg-bg-1 p-6" : "py-8",
        className,
      )}
    >
      <div className="flex flex-col items-center gap-3">
        {illustration ? (
          <EmptyIllustration
            kind={illustration}
            size={illustrationSize ?? (variant === "inline" ? 120 : 160)}
            className="mx-auto"
          />
        ) : (
          <span
            className="mx-auto grid h-9 w-9 shrink-0 place-items-center rounded-2 border border-line-2 bg-bg-2 text-fg-3"
            aria-hidden
          >
            <Glyph icon={Icon} size="md" />
          </span>
        )}
        <div className="min-w-0 text-center">
          <h3 className="flex items-center justify-center gap-1 text-[14px] font-semibold text-fg-0">
            {title}
            {helpId && renderHelp(helpId)}
          </h3>
          {body && (
            <p className="mx-auto mt-1.5 max-w-2xl text-[13px] leading-relaxed text-fg-2">
              {body}
            </p>
          )}
          {steps && steps.length > 0 && (
            <ol className="mt-3 inline-block space-y-1.5 text-left text-[12.5px] text-fg-2">
              {steps.map((s, i) => (
                <li key={i} className="flex items-baseline gap-2">
                  <span className="font-mono text-[10.5px] text-fg-4">
                    {i + 1}
                  </span>
                  <span>{s}</span>
                </li>
              ))}
            </ol>
          )}
          {snippet && (
            <InfoDisclosure summary={snippetLabel ?? "Learn more"} className="mx-auto mt-3 max-w-2xl text-left">
              <SnippetBlock snippet={snippet} />
            </InfoDisclosure>
          )}
          {children && <div className="mx-auto mt-3 max-w-2xl text-left">{children}</div>}
          {(to || guideId || notifyOperators || docHint) && (
            <div className="mt-4 flex flex-wrap items-center justify-center gap-3">
              {to && (
                <Link
                  to={to}
                  className="rounded-2 border border-accent/40 bg-accent-soft px-3 py-1.5 text-[12px] font-semibold text-accent hover:opacity-90"
                >
                  {toLabel}
                </Link>
              )}
              {guideId && (
                <button
                  type="button"
                  data-help-id={guideId}
                  className="rounded-2 border border-line-2 bg-bg-2 px-3 py-1.5 text-[12px] font-semibold text-fg-1 hover:border-line-3 hover:bg-bg-3"
                >
                  Read the guide
                </button>
              )}
              {notifyOperators && (
                <Link
                  to="/announcements"
                  className="text-[12px] font-medium text-fg-3 underline decoration-line-3 underline-offset-2 hover:text-fg-1"
                >
                  Notify node operators
                </Link>
              )}
              {docHint && (
                <span className="text-[11.5px] text-fg-4">
                  Docs: <code className="font-mono text-fg-3">{docHint}</code>
                </span>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// SnippetBlock renders a copyable config snippet (e.g. a config.toml block)
// with a one-click copy button and transient "Copied" feedback.
function SnippetBlock({ label, snippet }: { label?: string; snippet: string }) {
  const [copied, setCopied] = useState(false);
  async function copy() {
    try {
      await navigator.clipboard.writeText(snippet);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard access can be denied by the browser; the snippet is still
      // selectable/readable in the block, so fail silently.
    }
  }
  return (
    <div className="rounded-2 border border-line-2 bg-bg-2">
      {label && (
        <div className="border-b border-line-2 px-3 py-1.5 text-[11px] font-medium text-fg-3">
          {label}
        </div>
      )}
      <div className="flex items-start justify-between gap-2 p-3">
        <pre className="m-0 min-w-0 flex-1 overflow-x-auto whitespace-pre-wrap break-words font-mono text-[12px] leading-relaxed text-fg-1">
          {snippet}
        </pre>
        <button
          type="button"
          onClick={copy}
          aria-label="Copy snippet"
          className={clsx(
            "grid h-6 w-6 shrink-0 place-items-center rounded-1 border transition-colors",
            copied
              ? "border-success/40 text-success"
              : "border-line-2 text-fg-3 hover:border-line-3 hover:text-fg-1",
          )}
        >
          <Glyph icon={copied ? Check : Copy} size="xs" />
        </button>
      </div>
    </div>
  );
}
