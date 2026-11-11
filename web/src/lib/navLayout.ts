import type { NavGroup, NavItem } from "./nav.ts";

// Where each page sits in the sidebar. nav.ts stays the one table of pages
// (ids, paths, labels and icons: a governance contract, and upstream's); this
// file only decides placement, so the sidebar shows the few pages most people
// open every day and folds the rest under one "Advanced" section:
//
//   main      always listed, no heading
//   advanced  folded by default, in three headed sub-groups; a page a person
//             uses often can be kept in the main list ("pins")
//   foot      the account and settings strip at the bottom
//
// A page this file does not name (one a later upstream release adds) lands
// in Advanced by its nav.ts group (GROUP_FALLBACK), so a merge never drops a
// page from the sidebar and never grows the main list on its own.

export type NavTier = "main" | "advanced" | "foot";

export type NavSection = NavGroup & {
  tier: NavTier;
  /** The Advanced heading (the fold control) renders above this section. */
  toggle?: boolean;
};

/** The pages listed directly, in this order. */
export const NAV_MAIN_IDS = [
  "overview",
  "briefing",
  "live",
  "sessions",
  "cost",
  "terminals",
  "security",
  "code-security",
] as const;

/** The bottom strip, in this order. */
export const NAV_FOOT_IDS = ["messages", "requests", "you", "privacy", "settings"] as const;

/** The Advanced sub-groups and the pages each one names, in order. */
export const NAV_ADVANCED: { id: string; label: string; items: string[] }[] = [
  {
    id: "insights",
    label: "Insights",
    items: [
      "analysis",
      "actions",
      "projects",
      "tools",
      "search",
      "plans",
      "discovery",
      "patterns",
      "benchmarks",
      "modelfit",
      "apiexplorer",
      "licenses",
      "guide",
    ],
  },
  {
    id: "optimize",
    label: "Optimize",
    items: ["suggestions", "compression", "cache", "routing"],
  },
  {
    id: "control",
    label: "Control",
    items: ["egress", "policies", "agent-config", "remote"],
  },
];

/** Where a page this file does not name goes, by its nav.ts group. */
const GROUP_FALLBACK: Record<string, string> = {
  monitor: "insights",
  analyze: "insights",
  optimize: "optimize",
  configure: "control",
};

const MAIN = new Set<string>(NAV_MAIN_IDS);
const FOOT = new Set<string>(NAV_FOOT_IDS);
const NAMED = new Map<string, string>(
  NAV_ADVANCED.flatMap((s) => s.items.map((id) => [id, s.id] as [string, string])),
);

/** The Advanced sub-group of a page, or null for a main or foot page. */
export function advancedGroupOf(id: string, navGroupId: string): string | null {
  if (MAIN.has(id) || FOOT.has(id)) return null;
  return NAMED.get(id) ?? GROUP_FALLBACK[navGroupId] ?? "insights";
}

export type NavLayout = {
  /** What the sidebar renders, top to bottom. */
  sections: NavSection[];
  /** Every page that belongs to Advanced (kept in the main list or not), in
   *  Advanced order: what the "keep in the main list" choices offer. */
  choices: { item: NavItem; pinned: boolean }[];
  /** Advanced pages not kept in the main list: what the fold hides. */
  folded: number;
};

/**
 * layoutNav places the pages of `groups` (nav.ts groups, already filtered to
 * what this machine may show) into sidebar sections.
 *
 * - `pins`: Advanced pages the person keeps in the main list.
 * - `open`: whether Advanced is unfolded.
 * - `pathname`: the current route. With Advanced folded, the page being
 *   viewed still shows under the Advanced heading, so the current page is
 *   never missing from the sidebar (and a tour step that walks to an
 *   Advanced page finds its anchor).
 */
export function layoutNav(
  groups: NavGroup[],
  opts: { pins?: readonly string[]; open?: boolean; pathname?: string } = {},
): NavLayout {
  const pins = new Set(opts.pins ?? []);
  const byId = new Map<string, NavItem>();
  const sub = new Map<string, string>(); // advanced page id → sub-group id
  for (const g of groups) {
    for (const it of g.items) {
      byId.set(it.id, it);
      const a = advancedGroupOf(it.id, g.id);
      if (a) sub.set(it.id, a);
    }
  }
  const pick = (ids: readonly string[]) =>
    ids.map((id) => byId.get(id)).filter((it): it is NavItem => it != null);

  // Advanced order: the named pages in their listed order, then the pages
  // that arrived by fallback, in nav.ts order.
  const advanced = NAV_ADVANCED.map((s) => {
    const named = pick(s.items);
    const rest = [...sub].filter(([id, a]) => a === s.id && !NAMED.has(id)).map(([id]) => byId.get(id)!);
    return { id: s.id, label: s.label, items: [...named, ...rest] };
  });
  const all = advanced.flatMap((s) => s.items);
  const choices = all.map((item) => ({ item, pinned: pins.has(item.id) }));
  const folded = choices.filter((c) => !c.pinned).length;

  const sections: NavSection[] = [];
  const main = [...pick(NAV_MAIN_IDS), ...all.filter((it) => pins.has(it.id))];
  if (main.length > 0) sections.push({ id: "main", label: "", items: main, tier: "main" });

  if (folded > 0) {
    if (opts.open) {
      const shown = advanced
        .map((s) => ({ ...s, items: s.items.filter((it) => !pins.has(it.id)) }))
        .filter((s) => s.items.length > 0);
      shown.forEach((s, i) => sections.push({ ...s, tier: "advanced", toggle: i === 0 }));
    } else {
      const current = all.find((it) => !pins.has(it.id) && it.path === opts.pathname);
      sections.push({
        id: "advanced",
        label: "",
        items: current ? [current] : [],
        tier: "advanced",
        toggle: true,
      });
    }
  }

  const foot = pick(NAV_FOOT_IDS);
  if (foot.length > 0) sections.push({ id: "foot", label: "", items: foot, tier: "foot" });
  return { sections, choices, folded };
}

/**
 * navCrumb is the breadcrumb's first segment for a page: its Advanced
 * sub-group, or nothing for a main or foot page (the page name alone says
 * where it is). Pins do not change it: a page keeps one address.
 */
export function navCrumb(groups: NavGroup[], pathname: string): { label: string; item: NavItem } | null {
  for (const g of groups) {
    const item = g.items.find((i) => i.path === pathname);
    if (!item) continue;
    const a = advancedGroupOf(item.id, g.id);
    return { label: NAV_ADVANCED.find((s) => s.id === a)?.label ?? "", item };
  }
  return null;
}

// --- What a person chose, kept in this browser -----------------------------

const OPEN_KEY = "sb_nav_advanced";
const PINS_KEY = "sb_nav_pins";

export function readNavOpen(): boolean {
  try {
    return localStorage.getItem(OPEN_KEY) === "1";
  } catch {
    return false;
  }
}

export function writeNavOpen(open: boolean): void {
  try {
    localStorage.setItem(OPEN_KEY, open ? "1" : "0");
  } catch {
    /* best effort */
  }
}

/** parsePins reads a stored pin list, dropping anything that is not an id. */
export function parsePins(raw: string | null): string[] {
  if (!raw) return [];
  try {
    const v: unknown = JSON.parse(raw);
    if (!Array.isArray(v)) return [];
    return [...new Set(v.filter((x): x is string => typeof x === "string" && /^[a-z0-9-]{1,40}$/.test(x)))].slice(0, 40);
  } catch {
    return [];
  }
}

export function readNavPins(): string[] {
  try {
    return parsePins(localStorage.getItem(PINS_KEY));
  } catch {
    return [];
  }
}

export function writeNavPins(pins: readonly string[]): void {
  try {
    localStorage.setItem(PINS_KEY, JSON.stringify(pins));
  } catch {
    /* best effort */
  }
}
