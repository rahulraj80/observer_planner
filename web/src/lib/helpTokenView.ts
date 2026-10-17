// helpTokenView: the help registry's wording in the org's token-usage view
// (the developer cost display, shared/lib/format.ts cost affordances).
//
// Two layers, both applied by help.ts::helpRegistry() only in token-usage
// mode: (1) an override table for the entries whose help indicator stays
// visible on a page in that mode but whose text is about dollars (page tabs,
// the Usage headline, the Analysis charts that switch to tokens or turns, the
// budget card); (2) every other entry keeps its text with dollar FIGURES
// masked. Entries that document a dollar-only tile, column or chart stay in
// the drawer as reference: the tile itself is hidden in this mode, so no page
// points at them. Pure: no React, no DOM.

import type { HelpEntry } from "./help.ts";

/** The text fields an override may replace. */
export type HelpTextFields = Partial<
  Pick<HelpEntry, "title" | "oneLiner" | "detail" | "formula" | "example">
>;

const HIDDEN_NOTE =
  "Your organization shows token usage instead of costs on developer dashboards, so dollar figures are hidden.";

export const HELP_TOKEN_VIEW: Readonly<Record<string, HelpTextFields>> = {
  "tab.analysis": {
    oneLiner:
      "Usage insights for the selected window - headline KPIs, token trend, top movers, and context signals.",
    detail: `Headline tiles cover token volume, output tokens, cache efficacy, high-context turns, active hours, top-model concentration, stale-read waste, the budget as a share of its cap, and sessions. The trend chart stacks token volume by model, project or tool. The 'when you work' heatmap counts turns by day of week and hour. Top movers show period-over-period change as a percentage. Routing suggestions list sessions where a smaller model would likely have done the same work. ${HIDDEN_NOTE}`,
  },
  "tab.cost": {
    title: "Usage tab",
    oneLiner: "Per-model token consumption over the selected window.",
    detail: `Tabs under one KPI strip: Overview (token usage over time), By model (the four billable token buckets - net input, cache read, cache write/creation, output - per model, with the token source), Budgets (each budget as a share of its cap), Statement (the monthly statement) and Reconciliation (drift against a provider's own report, as a percentage). ${HIDDEN_NOTE}`,
  },
  "tab.overview": {
    oneLiner: "High-level snapshot - KPI cards, token usage and actions over time, top models and tools.",
    detail:
      "Your first stop. Combines KPI tiles, two time-series charts (token usage and actions per day), and two top-N rollups (models by tokens, tools by action count) for the selected window. Use it to spot whether anything looks off before drilling into a tab.",
  },
  "tab.suggestions": {
    oneLiner: "Prescriptive recommendations - what to change and why.",
    detail: `The advisor engine walks your captured sessions and emits ranked suggestions across four categories: cost, latency, quality and hygiene. A card shows the recoverable minutes where its detector estimates them. Everything is computed locally on read - nothing is stored or sent anywhere. Cards can be snoozed (7 days) or dismissed. ${HIDDEN_NOTE}`,
  },
  "tab.projects": {
    oneLiner:
      "Per-project activity, commits, AI-vs-human lines, and prompt-to-commit alignment - is the work turning into shipped commits?",
    detail: `One row per project. Click a row to open the detail panel: Overview (KPI band + ROI proxy tiles, each with its own formula and caveat, never colour-graded), Activity (by session/tool/model/day/task/commit), Commits (the commit ledger with linked prompts), Prompts (prompt -> commit chains with a status and, opt-in, an LLM-graded delivered/missed alignment), Tasks, and Guidance. ${HIDDEN_NOTE}`,
  },
  "metric.cost_usd": {
    title: "Token usage",
    oneLiner: "Total tokens across the four billable buckets in the window.",
    detail: `Net input + cache read + cache write + output, summed over every captured turn in the window. The subtitle is the token source mix by turns: proxy (ground truth), transcript (parsed session logs) or mixed. ${HIDDEN_NOTE}`,
  },
  "chart.analysis_trend": {
    title: "Token trend (dimension toggle)",
    oneLiner: "Stacked bars of token volume per bucket, split by model / project / tool.",
    detail:
      "One bar per bucket across the window (Auto: 5 min up to 3h, hour up to 7 days, day up to 180 days, week beyond; pick another with the granularity control), segmented by the active dimension. The same dim toggle drives the movers table below.",
  },
  "chart.analysis_cost_by_hour": {
    title: "Turns by hour of day",
    oneLiner: "Heatmap of turns per hour of day × day of week across the window.",
    detail:
      "Surfaces your daily rhythm. Buckets are in YOUR browser time zone (sent as tz=; UTC when the server does not know the zone) over the selected window. Use it to spot heavy-hour concentrations.",
  },
  "chart.analysis_movers": {
    oneLiner: "Top 5 increases and top 5 decreases vs the prior period of equal length, as a percentage.",
    detail: `Same dim toggle as the trend chart above (model / project / tool). Rows are ranked by the change in priced usage and show the percentage change. The "new this period" table next to it lists keys present in the current period but absent from the prior. ${HIDDEN_NOTE}`,
  },
  "chart.analysis_new_entrants": {
    oneLiner: "Keys (model / project / tool) present in the current period but not the prior.",
    detail:
      "Companion to the movers table. New entrants are different from \"increases\" because there is no prior baseline to diff against - useful for spotting a new model your client started routing to.",
  },
  "chart.analysis_top_sessions": {
    title: "Top sessions",
    oneLiner: "The heaviest sessions in the window, with explanatory badges.",
    detail:
      "Aggregates the per-turn-deduped scan by session_id and ranks by priced usage. Badges call out why a session landed high: `opus` (any Opus turn), `lc_tier` (any LC-tier turn), `many_turns` (>30), `large_prompt` (>100K single-turn prompt). Click a session to see its full breakdown in the Sessions tab.",
  },
  "chart.cost_over_time": {
    title: "Token usage over time chart",
  },
  "card.budget": {
    oneLiner:
      "Advisory monthly budgets - global and per-project - shown as a share of each cap, with 80%/100% banners and a month-end pace. Never gates traffic.",
    detail: `Each budget shows month-to-date usage as a percentage of its cap and the linear month-end pace (MTD × days-in-month / days-elapsed) as a percentage of the cap. Crossing 80% or 100% shows a banner. Everything here is advisory: the observer never blocks or delays a request over budget. ${HIDDEN_NOTE} The caps themselves are edited in config.toml.`,
  },
  "tile.analysis.high_context": {
    oneLiner: "Turns whose prompt window exceeded 100K and 200K tokens.",
    detail:
      "Model-agnostic - counts turns purely by prompt window size (input + cache_read + cache_creation).",
    formula: "turns_over_200k = COUNT(turn) where Input + CacheRead + CacheCreation > 200_000",
  },
};

/** applyTokenView resolves one entry's token-usage wording: the override's
 *  fields where present, every text field's dollar figures masked. */
export function applyTokenView(e: HelpEntry, mask: (text: string) => string): HelpEntry {
  const o = HELP_TOKEN_VIEW[e.id] ?? {};
  const pick = (k: keyof HelpTextFields): string | undefined => {
    const v = o[k] ?? e[k];
    return v == null ? undefined : mask(v);
  };
  return {
    ...e,
    title: pick("title") ?? e.title,
    oneLiner: pick("oneLiner") ?? e.oneLiner,
    detail: pick("detail"),
    formula: pick("formula"),
    example: pick("example"),
    source: e.source,
  };
}
