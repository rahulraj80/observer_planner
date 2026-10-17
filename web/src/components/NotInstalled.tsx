import { EmptyState } from "@/components/primitives";
import { useCatalogRow } from "@/lib/useComponents";
import { distrosWith, stateCopy, type ComponentStateName } from "@/lib/componentState";

// NotInstalled: the page-level (and panel-level) state for a surface whose
// component is not active on this machine (modular packaging plan 7.3),
// generalised from ObsDisabled. It names the component and its level ("not
// in this build" or "installed but inactive") and offers the same action as
// the component's row on Settings > Components, so a direct URL visit never
// 404s blank and never ends in a "run this command" dead end.
export function NotInstalled({
  component,
  state = "available",
  variant = "card",
}: {
  /** The catalog component id. */
  component: string;
  /** Its resolver state; anything but absent and blocked reads as inactive. */
  state?: ComponentStateName | string;
  variant?: "card" | "inline";
}) {
  const row = useCatalogRow(component);
  const title = row?.title ?? component;
  const s: ComponentStateName = state === "absent" || state === "blocked" ? state : "available";
  const headline =
    s === "absent" ? `${title} is not in this build` : s === "blocked" ? `${title} is blocked` : `${title} is not installed`;
  return (
    <div data-testid="not-installed" className={variant === "card" ? "p-4 sm:p-6" : undefined}>
      <EmptyState
        illustration="setup"
        variant={variant}
        title={headline}
        body={stateCopy(s, title, distrosWith(row))}
        to={`/settings/components?component=${encodeURIComponent(component)}`}
        toLabel={s === "available" ? "Turn it on in Settings > Components" : "Open Settings > Components"}
      >
        {row?.about && <p className="mx-auto mt-2 max-w-xl text-small text-fg-3">{row.about}</p>}
      </EmptyState>
    </div>
  );
}
