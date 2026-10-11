import type { ReactNode } from "react";
import { BrandLoader } from "./BrandMark";

// RestartOverlay: the full-screen "reconnecting" scrim shown while a
// dashboard-triggered restart is in flight (the node daemon via
// useDaemonRestart, the org server via useOrgRestart). The app owns the
// restart and the reload; this owns the look. The scrim is the page colour
// (--bg-0) so the app behind it reads as covered, not dimmed.
export function RestartOverlay({ title, body }: { title: string; body: string }) {
  return (
    <div
      role="status"
      aria-live="polite"
      className="fixed inset-0 z-50 flex items-center justify-center bg-bg-0/90 p-6 backdrop-blur-sm"
    >
      <div className="w-full max-w-sm space-y-3 rounded-3 border border-line-2 bg-bg-2 p-6 text-center">
        <div className="text-[13px] font-semibold text-fg-1">{title}</div>
        <BrandLoader size={28} label={title} className="mx-auto" />
        <p className="text-[12px] leading-relaxed text-fg-3">{body}</p>
      </div>
    </div>
  );
}

// RestartPendingBar: the slim warn strip under the top bar that says a
// saved change applies on the next restart. The app supplies what is
// pending (`message`), any restart error, and its own actions (the node
// confirms with a live count of in-flight proxied requests and offers a
// dismiss; the org arms an in-place ConfirmButton), so the two restart
// flows keep their own semantics on one chrome.
export function RestartPendingBar({
  message,
  error,
  actions,
}: {
  message: ReactNode;
  error?: string | null;
  actions: ReactNode;
}) {
  return (
    <div
      role="status"
      className="flex items-center gap-2 border-b border-warn/30 bg-warn-soft px-4 py-1.5 text-[11.5px] text-fg-2"
    >
      <span className="shrink-0 font-semibold text-warn">Restart pending</span>
      <span className="min-w-0 truncate">
        {message}
        {error && <span className="ml-2 text-danger">- {error}</span>}
      </span>
      <div className="flex-1" />
      <div className="flex shrink-0 items-center gap-2">{actions}</div>
    </div>
  );
}
