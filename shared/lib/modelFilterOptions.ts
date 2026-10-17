// modelFilterOptions - the option list behind every dashboard's Model filter
// chip (the node FilterBar and the org TopBar; one owner, so both apps offer
// the same choices the same way).
//
// The source is the per-model rollup of the current selection (the node's
// /api/models, the org's /api/org/models, each scoped by window, tool and
// project but never by the model filter itself), so the picker lists exactly
// the models that produced turns under that selection. Rows the store could
// not attribute to a model ("<unknown>", "<synthetic>", empty, placeholder
// strings) are not offered: there is no model id to filter on, and offering
// "No model reported" as a choice would promise a filter the server cannot
// apply.
//
// A model that is already selected but has no rows under the current
// selection (the viewer picked it, then changed the tool or project) is never
// silently cleared: withSelectedModel keeps it as the first option, flagged
// noData, so the chip can say "no data in this selection".
//
// Pure (no React) so it runs under node --test.

import { modelFamily } from "./modelFamilies.ts";

/** ModelFilterOption is one selectable model. */
export type ModelFilterOption = {
  /** The model id exactly as the store holds it (the `model=` value). */
  model: string;
  /** Turns under the current selection, the picker's right-hand count. */
  turns: number;
  /** Lower-cased type-ahead text: the id plus its family and maker. */
  searchable: string;
  /** True for a selected model with no rows under the current selection. */
  noData?: boolean;
};

// Ids that mean "no model" on the wire (the cost engine's own sentinels and
// the placeholder strings shared/lib/models.ts formatModelId treats as
// unknown).
const NOT_A_MODEL = new Set(["", "<unknown>", "<synthetic>", "(none)", "(unknown)", "none", "unknown", "null", "-"]);

/** NO_DATA_NOTE is the copy for a kept selection with no rows. */
export const NO_DATA_NOTE = "No data in this selection";

function searchableFor(model: string): string {
  const fam = modelFamily(model);
  return [model, fam && !fam.hidden ? `${fam.label} ${fam.maker}` : ""].join(" ").trim().toLowerCase();
}

/**
 * modelFilterOptions turns per-model rollup rows into picker options: unknown
 * and placeholder ids dropped, duplicates merged, most-used first (ties by id
 * so the order is stable).
 */
export function modelFilterOptions(rows: readonly { key: string; turn_count?: number }[]): ModelFilterOption[] {
  const turns = new Map<string, number>();
  for (const r of rows) {
    const id = (r.key ?? "").trim();
    if (NOT_A_MODEL.has(id.toLowerCase())) continue;
    turns.set(id, (turns.get(id) ?? 0) + (r.turn_count ?? 0));
  }
  return [...turns.entries()]
    .map(([model, n]) => ({ model, turns: n, searchable: searchableFor(model) }))
    .sort((a, b) => b.turns - a.turns || a.model.localeCompare(b.model));
}

/**
 * withSelectedModel keeps the selected model visible: when `loaded` (the
 * option list for the current selection has arrived) and the selected model
 * ("all" = none) is not among `options`, it is prepended flagged noData.
 * While the list is still loading nothing is flagged, so a filter restored
 * from the URL never flashes "no data" before its data arrives.
 */
export function withSelectedModel(
  options: readonly ModelFilterOption[],
  selected: string,
  loaded: boolean,
): { options: ModelFilterOption[]; selectedMissing: boolean } {
  const sel = selected.trim();
  if (!loaded || sel === "" || sel === "all" || options.some((o) => o.model === sel)) {
    return { options: [...options], selectedMissing: false };
  }
  return {
    options: [{ model: sel, turns: 0, searchable: searchableFor(sel), noData: true }, ...options],
    selectedMissing: true,
  };
}
