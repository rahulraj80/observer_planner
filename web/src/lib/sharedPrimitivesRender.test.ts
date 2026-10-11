// Renders REAL shared primitives to static markup and pins four fixes the
// platform tabs page lanes asked for (lane F2, 2026-10-02):
//   - SectionRail LINK rows (`href` / `onSelect`): a real <a href> (so middle
//     click / "open in new tab" work), the external arrow exactly once, never
//     aria-current; rows without them are unchanged buttons.
//   - SectionNav `gutter`: the sticky row bleeds into the app's own gutter
//     ("org" = web2's px-3 / sm:px-6) instead of the hard-coded node one.
//   - DataTable grouped headers: a group's header cell spans its leaf columns
//     (colSpan) and an ungrouped column gets a placeholder, so they align.
//   - StatCard / HeroStat: the sub row reserves the sparkline gutter only when a
//     sparkline is drawn, and HeroStat sizes its value to the tile (a 2-column
//     StatStrip at 375 clipped "observe" at a fixed 44 px).
//
// Same harness as tabsRender.test.ts: the node runner cannot import .tsx, so
// the modules are bundled with the esbuild that ships with vite (react stays
// external so the test and the components share one React).
import { test, before } from "node:test";
import assert from "node:assert/strict";
import { mkdirSync } from "node:fs";
import { fileURLToPath, pathToFileURL } from "node:url";
import { build } from "esbuild";

type Mod = {
  SectionRail: (props: Record<string, unknown>) => unknown;
  SectionNav: (props: Record<string, unknown>) => unknown;
  DataTable: (props: Record<string, unknown>) => unknown;
  StatCard: (props: Record<string, unknown>) => unknown;
  HeroStat: (props: Record<string, unknown>) => unknown;
  heroValueFontSize: (value: unknown, hasUnit?: boolean) => string | undefined;
  headerColSpan: (h: { colSpan: number }) => number | undefined;
  createElement: (type: unknown, props?: Record<string, unknown> | null, ...children: unknown[]) => unknown;
  renderToStaticMarkup: (el: unknown) => string;
};

let m: Mod;

before(async () => {
  const webDir = fileURLToPath(new URL("../../", import.meta.url));
  const prim = (f: string) => JSON.stringify(fileURLToPath(new URL(`../../../shared/primitives/${f}`, import.meta.url)));
  const outDir = `${webDir}node_modules/.cache/shared-primitives-render-test`;
  mkdirSync(outDir, { recursive: true });
  const outfile = `${outDir}/prims.mjs`;
  await build({
    stdin: {
      contents: [
        `export { SectionRail } from ${prim("SectionRail.tsx")};`,
        `export { SectionNav } from ${prim("SectionNav.tsx")};`,
        `export { DataTable, headerColSpan } from ${prim("DataTable.tsx")};`,
        `export { StatCard } from ${prim("StatCard.tsx")};`,
        `export { HeroStat, heroValueFontSize } from ${prim("HeroStat.tsx")};`,
        `export { createElement } from "react";`,
        `export { renderToStaticMarkup } from "react-dom/server";`,
      ].join("\n"),
      resolveDir: webDir,
      loader: "ts",
    },
    bundle: true,
    format: "esm",
    platform: "node",
    jsx: "automatic",
    outfile,
    external: ["react", "react-dom", "react/jsx-runtime", "react-dom/server", "react-router-dom"],
    banner: { js: 'import { createRequire } from "node:module"; const require = createRequire(import.meta.url);' },
    logLevel: "silent",
  });
  m = (await import(pathToFileURL(outfile).href)) as Mod;
});

const h = (type: unknown, props: Record<string, unknown>) => m.renderToStaticMarkup(m.createElement(type, props));

// ---------------------------------------------------------------- SectionRail

const RAIL = [
  { id: "general", label: "General", items: [{ id: "server", label: "Server" }, { id: "display", label: "Display" }] },
  {
    id: "elsewhere",
    label: "Elsewhere",
    items: [
      { id: "go-posture", label: "Collection & posture", href: "/posture" },
      { id: "go-intel", label: "Session enrichment", href: "/intelligence?tab=session-enrichment", onSelect: () => {} },
      { id: "go-news", label: "Announcements", onSelect: () => {} },
    ],
  },
];

// The vertical rail's markup (the first <nav>; the chip row is the second).
function rail(value = "server"): string {
  const html = h(m.SectionRail, { groups: RAIL, value, onChange: () => {}, ariaLabel: "Settings sections" });
  return html.slice(0, html.indexOf("</nav>") + 6);
}

function rowOf(html: string, label: string): string {
  const at = html.indexOf(`>${label}<`);
  assert.ok(at >= 0, `${label} not rendered`);
  const start = Math.max(html.lastIndexOf("<a ", at), html.lastIndexOf("<button", at));
  const close = html.startsWith("<a ", start) ? "</a>" : "</button>";
  return html.slice(start, html.indexOf(close, at) + close.length);
}

const ARROW = /lucide-arrow-up-right/g;

test("SectionRail: an href row is a real link with the arrow once, never aria-current", () => {
  const html = rail("go-posture"); // even if the page passed a link id as value
  const row = rowOf(html, "Collection &amp; posture");
  assert.match(row, /^<a href="\/posture" data-rail-link=""/);
  assert.ok(!row.includes("aria-current"));
  assert.equal(row.match(ARROW)?.length, 1, "exactly one external arrow");
  assert.match(rowOf(html, "Session enrichment"), /^<a href="\/intelligence\?tab=session-enrichment"/);
});

test("SectionRail: an onSelect-only row is a button marked as a link", () => {
  const row = rowOf(rail(), "Announcements");
  assert.match(row, /^<button type="button" data-rail-link=""/);
  assert.equal(row.match(ARROW)?.length, 1);
});

test("SectionRail: a link row with its own glyph adds the arrow after the label", async () => {
  const { Bell } = await import("lucide-react");
  const html = h(m.SectionRail, {
    groups: [{ id: "g", items: [{ id: "s", label: "Server" }, { id: "n", label: "News", icon: Bell, href: "/news" }] }],
    value: "s",
    onChange: () => {},
  });
  const row = rowOf(html.slice(0, html.indexOf("</nav>")), "News");
  assert.match(row, /lucide-bell/);
  assert.equal(row.match(ARROW)?.length, 1);
  assert.ok(row.indexOf("lucide-bell") < row.indexOf(">News<") && row.indexOf(">News<") < row.search(ARROW));
});

test("SectionRail: plain section rows are unchanged buttons; the selected one is aria-current", () => {
  const html = rail("display");
  assert.match(rowOf(html, "Display"), /^<button type="button" aria-current="true" class="/);
  assert.match(rowOf(html, "Server"), /^<button type="button" class="/);
  assert.ok(!rowOf(html, "Server").includes("data-rail-link"));
  assert.ok(!rowOf(html, "Server").match(ARROW));
});

test("SectionRail: the chip row renders link chips as links too", () => {
  const html = h(m.SectionRail, { groups: RAIL, value: "server", onChange: () => {} });
  const chips = html.slice(html.indexOf("</nav>") + 6);
  assert.match(chips, /<a href="\/posture" data-rail-link=""/);
  assert.equal((chips.match(/aria-current="true"/g) ?? []).length, 1);
});

// ----------------------------------------------------------------- SectionNav

const NAV_ITEMS = [{ id: "a", label: "A" }, { id: "b", label: "B" }];
const navClass = (props: Record<string, unknown>) =>
  /<nav aria-label="On this page" class="([^"]*)"/.exec(h(m.SectionNav, { items: NAV_ITEMS, ...props }))![1];

test("SectionNav: default gutter is the node / portal one (unchanged for existing callers)", () => {
  const cls = navClass({ className: "mb-4" });
  assert.match(cls, /(^| )-mx-4 px-4 sm:-mx-6 sm:px-6( |$)/);
  assert.match(cls, / mb-4$/);
});

test("SectionNav: gutter=org matches web2's px-3 / sm:px-6 main, no overrides needed", () => {
  const cls = navClass({ gutter: "org" });
  assert.match(cls, /(^| )-mx-3 px-3 sm:-mx-6 sm:px-6( |$)/);
  assert.ok(!cls.includes("-mx-4") && !cls.includes(" px-4"));
});

test("SectionNav: gutter=none does not bleed", () => {
  const cls = navClass({ gutter: "none" });
  assert.ok(!/-mx-|(^| )px-\d/.test(cls), cls);
});

// ------------------------------------------------------------------ DataTable

test("DataTable: headerColSpan only sets colSpan above 1", () => {
  assert.equal(m.headerColSpan({ colSpan: 1 }), undefined);
  assert.equal(m.headerColSpan({ colSpan: 3 }), 3);
});

test("DataTable: grouped column headers span their leaves; ungrouped columns get a placeholder", () => {
  const columns = [
    { id: "dev", header: "Developer", accessorKey: "dev" },
    {
      id: "spend",
      header: "Spend",
      columns: [
        { id: "tokens", header: "Tokens", accessorKey: "tokens" },
        { id: "usd", header: "USD", accessorKey: "usd" },
      ],
    },
    {
      id: "edits",
      header: "Edits",
      columns: [
        { id: "ai", header: "AI", accessorKey: "ai" },
        { id: "human", header: "Human", accessorKey: "human" },
        { id: "files", header: "Files", accessorKey: "files" },
      ],
    },
  ];
  const html = h(m.DataTable, {
    data: [{ dev: "ada", tokens: 1, usd: 2, ai: 3, human: 4, files: 5 }],
    columns,
    rowKey: (r: { dev: string }) => r.dev,
  });
  const thead = html.slice(html.indexOf("<thead"), html.indexOf("</thead>"));
  const rows = thead.split("</tr>").filter((r) => r.includes("<th"));
  assert.equal(rows.length, 2, "a group row and a leaf row");
  const cells = (row: string) => row.match(/<th(?=[\s>])[^>]*>/g) ?? [];
  // Group row: placeholder over Developer, Spend spans 2, Edits spans 3.
  const top = cells(rows[0]);
  assert.equal(top.length, 3);
  assert.match(top[0], /aria-hidden="true"/);
  assert.ok(!/colSpan|colspan/.test(top[0]));
  assert.match(top[1], /colSpan="2"|colspan="2"/);
  assert.match(top[1], /scope="colgroup"/);
  assert.match(top[2], /colSpan="3"|colspan="3"/);
  assert.match(rows[0], />Spend</);
  assert.match(rows[0], />Edits</);
  // Leaf row: six cells, one per body column, none spanning.
  const leaf = cells(rows[1]);
  assert.equal(leaf.length, 6);
  for (const c of leaf) assert.ok(!/colSpan|colspan/.test(c), c);
  // The width the group row covers equals the leaf row's (they align).
  const covered = top.reduce((n, c) => n + Number(/colspan="(\d+)"/i.exec(c)?.[1] ?? 1), 0);
  assert.equal(covered, leaf.length);
});

test("DataTable: an ungrouped table renders one header row with no colSpan (unchanged)", () => {
  const html = h(m.DataTable, {
    data: [{ a: 1, b: 2 }],
    columns: [
      { id: "a", header: "A", accessorKey: "a" },
      { id: "b", header: "B", accessorKey: "b" },
    ],
    rowKey: (r: { a: number }) => String(r.a),
  });
  const thead = html.slice(html.indexOf("<thead"), html.indexOf("</thead>"));
  assert.equal((thead.match(/<tr/g) ?? []).length, 1);
  assert.ok(!/colspan|scope=|<th aria-hidden/i.test(thead));
});

// ------------------------------------------------------------------ StatCard

function subRow(props: Record<string, unknown>): string {
  const html = h(m.StatCard, { label: "Denied", value: "11", sub: "11 asked", animate: false, ...props });
  const at = html.indexOf("11 asked");
  const start = html.lastIndexOf('<div class="mt-1.5', at);
  return html.slice(start, html.indexOf(">", start) + 1);
}

test("StatCard: the sub row reserves the sparkline gutter only when a sparkline is drawn", () => {
  assert.ok(!subRow({}).includes("pr-[88px]"), "no spark");
  assert.ok(!subRow({ spark: [3] }).includes("pr-[88px]"), "one point draws no sparkline");
  assert.ok(subRow({ spark: [1, 2, 3] }).includes("pr-[88px]"), "with a sparkline");
});

// ------------------------------------------------------------------ HeroStat

function heroSub(props: Record<string, unknown>): string {
  const html = h(m.HeroStat, { label: "Mode", value: "observe", sub: "3 nodes", ...props });
  const at = html.indexOf("3 nodes");
  const start = html.lastIndexOf('<div class="mt-2', at);
  return html.slice(start, html.indexOf(">", start) + 1);
}

// [name, props, reserves the corner gutter]
const HERO_PAD: Array<[string, Record<string, unknown>, boolean]> = [
  ["no sparkline (corner mode, the default)", {}, false],
  ["a one-point series draws nothing", { spark: [4] }, false],
  ["a corner sparkline", { spark: [1, 3, 2] }, true],
  ["a wide sparkline sits below, not in the corner", { spark: [1, 3, 2], sparkMode: "wide" }, false],
  ["delta row without a sparkline", { delta: 0.12 }, false],
  ["delta row with a corner sparkline", { delta: 0.12, spark: [1, 2] }, true],
];
for (const [name, props, want] of HERO_PAD) {
  test(`HeroStat: ${name} -> ${want ? "reserves" : "no"} pr-[100px]`, () => {
    assert.equal(heroSub(props).includes("pr-[100px]"), want);
  });
}

// [value, hasUnit, want]
const HERO_SIZE: Array<[unknown, boolean, string | undefined]> = [
  ["observe", false, "min(44px, 23.04cqw)"], // fits a ~130 px half-width phone tile
  [82, false, "min(44px, 80.64cqw)"], // short: 44 px anywhere it fits
  ["$24,835.62", false, "min(44px, 16.12cqw)"],
  ["11", true, "min(44px, 40.32cqw)"], // a unit reserves two more characters
  ["", false, undefined],
  [{ type: "span" }, false, undefined], // a node (web2 <Money>): keeps the 44 px class
];
for (const [value, hasUnit, want] of HERO_SIZE) {
  test(`heroValueFontSize(${JSON.stringify(value)}, unit=${hasUnit}) -> ${want}`, () => {
    assert.equal(m.heroValueFontSize(value, hasUnit), want);
  });
}

test("HeroStat: the value row is an inline-size container and the value sizes to it", () => {
  const html = h(m.HeroStat, { label: "Mode", value: "observe" });
  assert.match(html, /<div class="mt-2 flex min-w-0 items-baseline gap-2" style="container-type:inline-size">/);
  assert.match(html, /class="min-w-0 text-\[44px\] font-bold[^"]*" style="font-size:min\(44px, 23.04cqw\)"/);
  // At 130 px of tile content that is 29.95 px: "observe" (7 glyphs) fits.
  const px = Math.min(44, (23.04 / 100) * 130);
  assert.ok(7 * 0.62 * px <= 130);
});
