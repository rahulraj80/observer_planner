// Renders the REAL shared TabStrip / TabPanel (shared/primitives/Tabs.tsx) to
// static markup and pins what a page lane relies on: hidden tabs are not
// rendered, the audited marker (eye glyph + "(audited)" in the accessible
// name) appears only on audited, usable tabs, the roving tabindex lands on the
// selected tab (or the first usable one when the selection is hidden), a
// disabled tab carries its reason, ids pair tab <-> panel, and a TabPanel whose
// tab was never activated renders nothing (its loaders cannot run).
//
// The node runner cannot import .tsx, so the module is bundled first with the
// esbuild that ships with vite (react stays external so the test and the
// component share one React). The keep-alive SWITCHING behaviour is pinned on
// the pure latch in tabsModel.test.ts; server rendering is one-shot.
import { test, before } from "node:test";
import assert from "node:assert/strict";
import { mkdirSync } from "node:fs";
import { fileURLToPath, pathToFileURL } from "node:url";
import { build } from "esbuild";

type Rendered = {
  TabStrip: (props: Record<string, unknown>) => unknown;
  TabPanel: (props: Record<string, unknown>) => unknown;
  createElement: (type: unknown, props?: Record<string, unknown> | null, ...children: unknown[]) => unknown;
  renderToStaticMarkup: (el: unknown) => string;
};

let m: Rendered;

before(async () => {
  const webDir = fileURLToPath(new URL("../../", import.meta.url));
  const tabs = fileURLToPath(new URL("../../../shared/primitives/Tabs.tsx", import.meta.url));
  const outDir = `${webDir}node_modules/.cache/tabs-render-test`;
  mkdirSync(outDir, { recursive: true });
  const outfile = `${outDir}/tabs.mjs`;
  await build({
    stdin: {
      contents: [
        `export { TabStrip, TabPanel } from ${JSON.stringify(tabs)};`,
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
    external: ["react", "react-dom", "react/jsx-runtime", "react-dom/server"],
    // A bundled CommonJS dependency require()s react: give the ESM bundle a
    // real require so it resolves the same external React.
    banner: { js: 'import { createRequire } from "node:module"; const require = createRequire(import.meta.url);' },
    logLevel: "silent",
  });
  m = (await import(pathToFileURL(outfile).href)) as Rendered;
});

const TABS = [
  { id: "overview", label: "Overview", count: 6 },
  { id: "sessions", label: "Sessions", audited: { actions: ["view_org_developers"] } },
  { id: "efficiency", label: "Efficiency", disabled: true, disabledReason: "Not configured on this server" },
  { id: "terminals", label: "Terminals", hidden: true, audited: { actions: ["view_terminals"] } },
  {
    id: "security",
    label: "Security",
    ariaLabel: "Security",
    audited: { actions: ["view_guard_posture", "view_guard_pins"] },
  },
];

function strip(value: string): string {
  return m.renderToStaticMarkup(
    m.createElement(m.TabStrip, { tabs: TABS, value, onChange: () => {}, idPrefix: "pd", ariaLabel: "Developer", className: "mb-4" }),
  );
}

// The opening tag of one tab button.
function tabTag(html: string, id: string): string {
  const at = html.indexOf(`id="pd-tab-${id}"`);
  assert.ok(at >= 0, `tab ${id} not rendered`);
  const start = html.lastIndexOf("<button", at);
  return html.slice(start, html.indexOf(">", at) + 1);
}

// Everything from one tab's opening tag to its closing tag.
function tabBody(html: string, id: string): string {
  const start = html.lastIndexOf("<button", html.indexOf(`id="pd-tab-${id}"`));
  return html.slice(start, html.indexOf("</button>", start));
}

test("a tablist with its accessible name; spacing class on the outer box", () => {
  const html = strip("sessions");
  assert.match(html, /^<div class="min-w-0 border-b border-line-2 mb-4">/);
  assert.match(html, /role="tablist" aria-label="Developer"/);
});

test("hidden tabs are not rendered", () => {
  const html = strip("sessions");
  assert.ok(!html.includes("pd-tab-terminals"));
  assert.ok(!html.includes("Terminals"));
});

test("audited marker: eye glyph + '(audited)' only on audited, usable tabs", () => {
  const html = strip("sessions");
  assert.equal(html.split("data-audited-marker").length - 1, 2, "sessions + security only");
  assert.match(tabBody(html, "sessions"), /data-audited-marker/);
  assert.match(tabBody(html, "sessions"), /<span class="sr-only">\(audited\)<\/span>/);
  assert.ok(!tabBody(html, "overview").includes("data-audited-marker"));
  // With an explicit ariaLabel the suffix joins the label instead.
  assert.match(tabTag(html, "security"), /aria-label="Security \(audited\)"/);
  assert.ok(!tabBody(html, "security").includes("sr-only"));
});

test("roving tabindex on the selected tab; aria-selected follows it", () => {
  const html = strip("sessions");
  assert.match(tabTag(html, "sessions"), /aria-selected="true"/);
  assert.match(tabTag(html, "sessions"), /tabindex="0"/);
  for (const id of ["overview", "efficiency", "security"]) {
    assert.match(tabTag(html, id), /tabindex="-1"/, id);
    assert.match(tabTag(html, id), /aria-selected="false"/, id);
  }
});

test("a hidden selection keeps the strip keyboard-reachable via the first usable tab", () => {
  const html = strip("terminals");
  assert.match(tabTag(html, "overview"), /tabindex="0"/);
  assert.ok(!html.includes('aria-selected="true"'));
});

test("a disabled tab carries its reason and no audited marker", () => {
  const tag = tabTag(strip("overview"), "efficiency");
  assert.match(tag, /disabled=""/);
  assert.match(tag, /aria-disabled="true"/);
  assert.match(tag, /title="Not configured on this server"/);
});

test("tab ids pair with panel ids; counts render from caller data", () => {
  const html = strip("overview");
  assert.match(tabTag(html, "overview"), /aria-controls="pd-panel-overview"/);
  assert.match(tabBody(html, "overview"), />6<\/span>/);
});

test("TabPanel: nothing before first activation, a labelled tabpanel when active", () => {
  const child = m.createElement("p", null, "audited content");
  const never = m.renderToStaticMarkup(
    m.createElement(m.TabPanel, { id: "security", active: "overview", idPrefix: "pd", children: child }),
  );
  assert.equal(never, "");
  const open = m.renderToStaticMarkup(
    m.createElement(m.TabPanel, { id: "security", active: "security", idPrefix: "pd", className: "mt-5", children: child }),
  );
  assert.equal(
    open,
    '<div id="pd-panel-security" role="tabpanel" aria-labelledby="pd-tab-security" class="mt-5"><p>audited content</p></div>',
  );
});
