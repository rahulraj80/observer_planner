// arenaModels is the pure option model behind the Arena tab's judge and
// candidate model pickers (components/arena/ArenaTab.tsx). The daemon's GET
// /api/arena/models composes each harness's groups server-side
// (internal/arenamodels); this module only turns one harness row into the
// ordered list of picker items the shared ComboChip renders, and owns the
// per-harness "last choice" preference.
//
// Rules this module owns:
//   - "Default" (the harness's own default) is ALWAYS the first item, and
//     names the routed lane's default when the daemon reports one.
//   - Group labels and order follow the server's groups: used on this
//     machine, then available for the harness, then not routable. A disabled
//     row stays visible with its reason (never hidden).
//   - A harness the daemon marks not selectable offers only Default; the
//     caller disables the picker and shows the reason.
//   - A typed or remembered id that is not in the list stays visible as its
//     own row, so the chip never shows a value the list cannot explain.
//
// No imports besides `import type`, so it runs under the node test runner.

export type ArenaModelGroup = "used" | "available" | "unroutable";

export type ArenaModelSource = "tool_history" | "node_history" | "alias" | "catalog";

/** One option row of GET /api/arena/models (arenamodels.Option). */
export type ArenaModelOption = {
  model: string;
  group: ArenaModelGroup;
  source: ArenaModelSource;
  count?: number;
  last_used?: string;
  tools?: string[];
  used_as?: string;
  disabled?: boolean;
  reason?: string;
};

/** One harness row of GET /api/arena/models. */
export type ArenaHarnessModels = {
  tool: string;
  selectable: boolean;
  reason?: string;
  catalog: string;
  providers?: string[];
  default_model?: string;
  note?: string;
  options: ArenaModelOption[];
  installed: boolean;
};

export type ArenaModelsResponse = {
  routed: boolean;
  harnesses: ArenaHarnessModels[];
};

/** The sentinel value of the "Other model id" row. */
export const CUSTOM_MODEL = "__custom__";

/** One picker row, already worded; the component maps it to a ComboOption. */
export type ArenaModelItem = {
  value: string;
  name: string;
  detail: string;
  group?: string;
  disabled?: boolean;
  title?: string;
  searchable: string;
};

// PROVIDER_LABELS is display copy only (the family decision is server-side).
const PROVIDER_LABELS: Readonly<Record<string, string>> = {
  anthropic: "Anthropic",
  openai: "OpenAI",
  xai: "xAI",
  google: "Google",
};

// GROUP_LABELS is the one table of section headings, keyed by server group.
const GROUP_LABELS: Readonly<Record<ArenaModelGroup, (tool: string) => string>> = {
  used: () => "Used on this machine",
  available: (tool) => `Available for ${tool}`,
  unroutable: () => "Not routable in Arena",
};

/** providerLabel names a harness's provider families for copy. */
export function providerLabel(providers: string[] | undefined): string {
  return (providers ?? []).map((p) => PROVIDER_LABELS[p] ?? p).join(" / ");
}

/** shortDate renders an RFC3339 timestamp as YYYY-MM-DD ("" when absent). */
export function shortDate(iso: string | undefined): string {
  if (!iso) return "";
  const m = /^(\d{4}-\d{2}-\d{2})/.exec(iso);
  return m ? m[1] : "";
}

function historyDetail(o: ArenaModelOption): string {
  const parts: string[] = [];
  if (o.count) parts.push(`${o.count.toLocaleString("en-US")} turn${o.count === 1 ? "" : "s"}`);
  const d = shortDate(o.last_used);
  if (d) parts.push(`last used ${d}`);
  if (o.tools && o.tools.length > 0) parts.push(`by ${o.tools.join(", ")}`);
  if (o.used_as) parts.push(`recorded as ${o.used_as}`);
  return parts.join(" · ");
}

function optionDetail(o: ArenaModelOption, h: ArenaHarnessModels): string {
  if (o.disabled) return o.reason ?? "Not available for this harness";
  switch (o.source) {
    case "tool_history":
    case "node_history":
      return historyDetail(o);
    case "alias":
      return `Alias ${h.tool} resolves to its current model`;
    case "catalog": {
      const p = providerLabel(h.providers);
      return p ? `${p} model in the price catalog` : "Model in the price catalog";
    }
    default:
      return "";
  }
}

/** defaultDetail explains what the Default row runs. */
export function defaultDetail(h: ArenaHarnessModels | undefined, tool: string): string {
  if (h?.default_model) return `Arena runs ${h.default_model}, this lane's routed default`;
  return `${tool}'s own default model`;
}

/**
 * buildArenaModelItems turns one harness row into the ordered picker items:
 * Default, then the server's groups in order, then a Custom group (a typed or
 * remembered value the list does not carry, and the "Other model id" row).
 */
export function buildArenaModelItems(
  h: ArenaHarnessModels | undefined,
  tool: string,
  current: string,
): ArenaModelItem[] {
  const items: ArenaModelItem[] = [
    {
      value: "",
      name: "Default",
      detail: defaultDetail(h, tool),
      searchable: `default ${h?.default_model ?? ""}`.toLowerCase(),
    },
  ];
  if (!h || !h.selectable) return items;

  const order: ArenaModelGroup[] = ["used", "available", "unroutable"];
  const seen = new Set<string>([""]);
  for (const g of order) {
    for (const o of h.options) {
      if (o.group !== g || seen.has(o.model)) continue;
      seen.add(o.model);
      const detail = optionDetail(o, h);
      items.push({
        value: o.model,
        name: o.model,
        detail,
        group: GROUP_LABELS[g](tool),
        disabled: o.disabled || undefined,
        title: o.disabled ? o.reason : undefined,
        searchable: `${o.model} ${o.used_as ?? ""} ${(o.tools ?? []).join(" ")} ${detail}`.toLowerCase(),
      });
    }
  }

  const typed = current.trim();
  if (typed && typed !== CUSTOM_MODEL && !seen.has(typed)) {
    items.push({
      value: typed,
      name: typed,
      detail: "Typed or remembered id, not in this machine's history",
      group: "Custom",
      searchable: typed.toLowerCase(),
    });
  }
  items.push({
    value: CUSTOM_MODEL,
    name: "Other model id…",
    detail: `Type an id ${tool} accepts`,
    group: "Custom",
    searchable: "other custom type model id",
  });
  return items;
}

/**
 * modelForSubmit is the value the run request carries: "" for Default, for a
 * harness that cannot take a model, for the bare Custom sentinel or for a
 * disabled row; otherwise the trimmed id.
 */
export function modelForSubmit(h: ArenaHarnessModels | undefined, value: string): string {
  const v = value.trim();
  if (!v || v === CUSTOM_MODEL) return "";
  if (h && !h.selectable) return "";
  if (h?.options.some((o) => o.model === v && o.disabled)) return "";
  return v;
}

/** modelLabel renders a recorded model for a scorecard ("" = harness default). */
export function modelLabel(model: string | undefined): string {
  return model && model.trim() ? model : "harness default";
}

// ---- per-harness last-choice preference (per-viewer convenience) ----

export const ARENA_PREFS_KEY = "sbo.arena.prefs.v1";

export type ArenaPrefs = {
  judgeTool?: string;
  tools?: string[];
  /** candidate model per harness */
  models: Record<string, string>;
  /** judge model per judge harness */
  judgeModels: Record<string, string>;
};

function stringMap(v: unknown): Record<string, string> {
  const out: Record<string, string> = {};
  if (!v || typeof v !== "object" || Array.isArray(v)) return out;
  for (const [k, val] of Object.entries(v as Record<string, unknown>)) {
    if (typeof val === "string" && val !== CUSTOM_MODEL) out[k] = val;
  }
  return out;
}

/** parseArenaPrefs reads a stored preference blob, tolerating any garbage. */
export function parseArenaPrefs(raw: string | null | undefined): ArenaPrefs {
  const empty: ArenaPrefs = { models: {}, judgeModels: {} };
  if (!raw) return empty;
  let doc: unknown;
  try {
    doc = JSON.parse(raw);
  } catch {
    return empty;
  }
  if (!doc || typeof doc !== "object" || Array.isArray(doc)) return empty;
  const d = doc as Record<string, unknown>;
  const prefs: ArenaPrefs = { models: stringMap(d.models), judgeModels: stringMap(d.judgeModels) };
  if (typeof d.judgeTool === "string" && d.judgeTool) prefs.judgeTool = d.judgeTool;
  if (Array.isArray(d.tools)) {
    const tools = d.tools.filter((t): t is string => typeof t === "string" && t !== "");
    if (tools.length > 0) prefs.tools = tools;
  }
  return prefs;
}

type ReadStore = { getItem(key: string): string | null };
type WriteStore = { setItem(key: string, value: string): void };

/** loadArenaPrefs reads the preference; an unavailable store yields defaults. */
export function loadArenaPrefs(store: ReadStore | undefined): ArenaPrefs {
  try {
    return parseArenaPrefs(store?.getItem(ARENA_PREFS_KEY));
  } catch {
    return { models: {}, judgeModels: {} };
  }
}

/** saveArenaPrefs writes the preference; a failing store is ignored. */
export function saveArenaPrefs(store: WriteStore | undefined, prefs: ArenaPrefs): void {
  try {
    store?.setItem(ARENA_PREFS_KEY, JSON.stringify(prefs));
  } catch {
    // private window / blocked storage: the preference is a convenience only
  }
}
