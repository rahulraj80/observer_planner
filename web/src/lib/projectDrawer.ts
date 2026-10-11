// projectDrawer - the Projects drawer's filter contract (REVIEW-3 P3-25,
// T-485). The drawer reads the page's global window (the same since / until,
// hours or days the list sends), so a closed range in the filter bar bounds
// every tab. The drawer's routes (/api/project/{id} and its children) are
// project-grain and do not narrow by tool or model, so when either filter is
// set the drawer says so instead of silently showing every tool and model.
//
// Pure (no React) so it runs under node --test.

import type { WindowParams } from "./filters";

export type { WindowParams };

/** windowKey is a stable dependency key for a window value (a fresh object
 * each render must not refetch). */
export function windowKey(win: WindowParams): string {
  if ("since" in win) return `since=${win.since}&until=${win.until}`;
  if ("hours" in win) return `hours=${win.hours}`;
  return `days=${win.days}`;
}

function isSet(v: string | undefined): boolean {
  return v != null && v !== "" && v !== "all";
}

/**
 * projectDrawerFilterNote names the set filters the drawer does not apply
 * ("" when none): "The tool filter does not apply to this drawer: it shows
 * every tool in the project", or the model / both variants.
 */
export function projectDrawerFilterNote(tool: string | undefined, model: string | undefined): string {
  const t = isSet(tool);
  const m = isSet(model);
  if (t && m) return "Tool and model filters do not apply to this drawer: it shows every tool and model in the project";
  if (t) return "The tool filter does not apply to this drawer: it shows every tool in the project";
  if (m) return "The model filter does not apply to this drawer: it shows every model in the project";
  return "";
}
