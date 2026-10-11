import { useState, type ReactNode } from "react";
import { X, type LucideIcon } from "lucide-react";
import { Icon } from "./Icon";

// OnboardingCard: the first-run welcome + checklist card both dashboards
// show on an empty Overview (the node before its first session, the org
// before its first enrolled push), so the first minute teaches the steps
// that light the page up instead of reading "no data". Pure chrome: the
// app decides WHEN to render it (it returns null itself once data exists)
// and supplies the content; steps link through the app's own router (the
// `cta` node), so shared stays free of router coupling. Dismissal is a
// per-viewer convenience in localStorage under the app's own key.

export type OnboardingStep = {
  key: string;
  icon: LucideIcon;
  /** The step's call to action, usually the app's <Link>. */
  cta: ReactNode;
  body: ReactNode;
};

function readDismissed(key: string): boolean {
  try {
    return localStorage.getItem(key) === "1";
  } catch {
    return false;
  }
}

export function OnboardingCard({
  storageKey,
  visual,
  title,
  intro,
  steps,
  actions,
  footer,
}: {
  /** localStorage flag that remembers a dismissal ("1"). */
  storageKey: string;
  /** The leading visual (the mascot, a glyph tile). */
  visual: ReactNode;
  title: ReactNode;
  intro: ReactNode;
  steps: readonly OnboardingStep[];
  /** A row under the steps (primary button, CLI twin). */
  actions?: ReactNode;
  /** A trailing line (a guide link, the demo-data offer). */
  footer?: ReactNode;
}) {
  const [hidden, setHidden] = useState(() => readDismissed(storageKey));
  if (hidden) return null;

  const dismiss = () => {
    try {
      localStorage.setItem(storageKey, "1");
    } catch {
      // Storage unavailable: hidden for this page load only.
    }
    setHidden(true);
  };

  return (
    <section className="relative overflow-hidden rounded-3 border border-accent/30 bg-bg-2 p-5">
      <span
        aria-hidden
        className="pointer-events-none absolute inset-0"
        style={{ background: "radial-gradient(circle at 100% 0%, var(--accent-soft), transparent 55%)" }}
      />
      <div className="relative flex items-start gap-4">
        <span className="shrink-0">{visual}</span>
        <div className="min-w-0 flex-1">
          <h2 className="text-[15px] font-semibold text-fg-0">{title}</h2>
          <p className="mt-1 max-w-2xl text-[12.5px] leading-relaxed text-fg-2">{intro}</p>
          <ol className="mt-4 grid gap-2.5 sm:grid-cols-2">
            {steps.map((s) => (
              <li key={s.key} className="flex items-start gap-2.5 rounded-2 border border-line-2 bg-bg-1 p-3">
                <span className="grid h-7 w-7 shrink-0 place-items-center rounded-2 border border-line-2 bg-bg-2 text-fg-3">
                  <Icon icon={s.icon} size="sm" />
                </span>
                <div className="min-w-0">
                  <div className="text-[12.5px] font-semibold">{s.cta}</div>
                  <p className="mt-0.5 text-[11.5px] leading-snug text-fg-3">{s.body}</p>
                </div>
              </li>
            ))}
          </ol>
          {actions && <div className="mt-3 flex flex-wrap items-center gap-3">{actions}</div>}
          {footer && <div className="mt-3 border-t border-line-2 pt-3 text-[11.5px] text-fg-3">{footer}</div>}
        </div>
        <button
          type="button"
          onClick={dismiss}
          className="relative grid h-7 w-7 shrink-0 place-items-center rounded-2 text-fg-4 hover:bg-bg-3 hover:text-fg-2"
          aria-label="Dismiss welcome"
        >
          <Icon icon={X} size="sm" />
        </button>
      </div>
    </section>
  );
}
