// filterScope - which of the global FilterBar controls a route actually
// reads. The bar used to show window / tool / project / model everywhere, including
// Settings, Terminals and Remote where they did nothing; a control that
// changes nothing on the page in front of you is a lie. This table is the one
// owner of that mapping: a page that starts reading a filter adds its row.
//
// Pure (no React, no router) so it runs under node --test.

/** FilterScope names the global filters a route consumes. */
export type FilterScope = { window: boolean; tool: boolean; project: boolean; model: boolean };

const ALL: FilterScope = { window: true, tool: true, project: true, model: true };
const NONE: FilterScope = { window: false, tool: false, project: false, model: false };

// First path segment -> the filters that page reads (grounded in each
// page's useFilters() destructure). Anything unlisted reads none.
const ROUTE_SCOPES: Record<string, FilterScope> = {
  "": ALL, // Overview
  actions: ALL,
  analysis: ALL,
  cache: ALL,
  compression: ALL,
  cost: ALL,
  discovery: ALL,
  sessions: ALL,
  suggestions: ALL,
  tools: ALL,
  // Patterns are mined per project: tool and model both narrow to projects
  // with a matching session (the server's project-grain rule).
  patterns: { window: false, tool: true, project: true, model: true },
  // The list honours the whole bar (internal/intelligence/dashboard
  // projectListQuery): window, tool, model and the one-project filter.
  projects: ALL,
  // Decisions, savings, shadow and health narrow by model (a decision is in
  // a model when it asked for it OR ran on it); the rows carry no tool /
  // project filter yet.
  routing: { window: true, tool: false, project: false, model: true },
  // The live strip has its own 15-minute window; it keeps the sessions
  // that used the chosen model.
  live: { window: false, tool: false, project: false, model: true },
  // Runs whose config matrix ran the chosen model.
  benchmarks: { window: false, tool: false, project: false, model: true },
};

// TAB_SCOPES overrides ROUTE_SCOPES for one tab of a page (the tab's `?tab=`
// value), walked top-down; the first row whose segment and tab match wins.
// Only a tab that reads a DIFFERENT set of filters than its page has a row.
const TAB_SCOPES: readonly { seg: string; tab: string; scope: FilterScope }[] = [
  // The monthly statement is a calendar month with its own project picker:
  // only the Model filter applies to it (/api/report/monthly model=).
  { seg: "cost", tab: "statement", scope: { window: false, tool: false, project: false, model: true } },
];

/**
 * filterScopeFor returns the filters the page at `pathname` (and, for a
 * tabbed page, its `search` string's `?tab=`) reads.
 */
export function filterScopeFor(pathname: string, search = ""): FilterScope {
  const seg = pathname.replace(/^\/+/, "").split("/")[0] ?? "";
  const tab = new URLSearchParams(search).get("tab") ?? "";
  if (tab) {
    const row = TAB_SCOPES.find((r) => r.seg === seg && r.tab === tab);
    if (row) return row.scope;
  }
  return ROUTE_SCOPES[seg] ?? NONE;
}

// A tool / project / model filter is "set" when it narrows to something: the
// FilterProvider's unfiltered value is "all" (lib/filters.tsx).
function isSet(v: string): boolean {
  return v !== "" && v !== "all";
}

/**
 * ignoredFilters lists the filters that are SET (narrowed, not "all") but not
 * read by this page, so the bar can say so instead of silently dropping them.
 */
export function ignoredFilters(
  scope: FilterScope,
  values: { tool: string; project: string; model?: string },
): IgnorableFilter[] {
  const out: IgnorableFilter[] = [];
  if (!scope.tool && isSet(values.tool)) out.push("tool");
  if (!scope.project && isSet(values.project)) out.push("project");
  if (!scope.model && isSet(values.model ?? "")) out.push("model");
  return out;
}

/**
 * barNoteFilters is what the bar's honesty note names: the set filters the
 * page ignores (ignoredFilters) plus the Model filter whenever the page does
 * not read it. The Model chip is ALWAYS in the bar (D-323: one overarching
 * model filter), disabled where it does not apply, and a disabled control
 * must say why in visible copy (ComboChip's contract), set or not.
 */
export function barNoteFilters(
  scope: FilterScope,
  values: { tool: string; project: string; model?: string },
): IgnorableFilter[] {
  const out = ignoredFilters(scope, values);
  if (!scope.model && !out.includes("model")) out.push("model");
  return out;
}

/** IgnorableFilter names a narrowing filter a page may not read. */
export type IgnorableFilter = "tool" | "project" | "model";

/**
 * ignoredFiltersNote renders the filter bar's honesty note for the filters
 * ignoredFilters returned ("" when none): "The model filter does not apply
 * to this page", "Tool and project filters do not apply to this page",
 * "Tool, project and model filters do not apply to this page".
 */
export function ignoredFiltersNote(ignored: readonly IgnorableFilter[]): string {
  if (ignored.length === 0) return "";
  if (ignored.length === 1) return `The ${ignored[0]} filter does not apply to this page`;
  const names = ignored.map((n, i) => (i === 0 ? n[0].toUpperCase() + n.slice(1) : n));
  const list = names.length === 2 ? names.join(" and ") : `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`;
  return `${list} filters do not apply to this page`;
}
