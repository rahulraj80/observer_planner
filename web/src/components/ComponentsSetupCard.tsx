import { useState } from "react";
import { Link } from "react-router-dom";
import { Button } from "@/components/primitives";
import { useComponentState } from "@/lib/useComponents";

// ComponentsSetupCard: the dashboard's first-visit prompt (modular packaging
// plan 7.1) while no component selection is recorded on this machine. It
// says what is true (every component in this build is active, as before)
// and links to Settings > Components. Dismissed per browser; it never
// renders once a selection exists or while the state read is unknown.

const DISMISS_KEY = "sbo_components_setup_dismissed";

function readDismissed(): boolean {
  try {
    return globalThis.localStorage?.getItem(DISMISS_KEY) === "1";
  } catch {
    return false;
  }
}

export function ComponentsSetupCard() {
  const st = useComponentState();
  const [dismissed, setDismissed] = useState(readDismissed);
  if (dismissed || !st.data?.wired || st.data.view.configured) return null;
  const dismiss = () => {
    setDismissed(true);
    try {
      globalThis.localStorage?.setItem(DISMISS_KEY, "1");
    } catch {
      // Storage unavailable: hidden for this page view.
    }
  };
  return (
    <div
      role="note"
      data-testid="components-setup-card"
      className="flex flex-wrap items-center justify-between gap-3 rounded-3 border border-line-2 bg-bg-1 px-4 py-3"
    >
      <div className="min-w-0 text-small text-fg-2">
        <span className="font-medium text-fg-1">Review what Observer runs on this machine.</span> Every
        component in this build is active. You can turn off what you do not use; an inactive component
        starts nothing and writes nothing into your tools.
      </div>
      <div className="flex shrink-0 gap-2">
        <Link
          to="/settings/components"
          className="rounded-2 border border-accent/50 bg-accent/15 px-3 py-1 text-small font-medium text-accent hover:bg-accent/25"
        >
          Review components
        </Link>
        <Button size="sm" variant="ghost" onClick={dismiss}>
          Dismiss
        </Button>
      </div>
    </div>
  );
}
