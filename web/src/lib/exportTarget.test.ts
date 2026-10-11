import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { EXPORT_ROUTES, NO_EXPORT_NOTE, TABBED_PAGES, exportTargetFor, type ExportFilterValues } from "./exportTarget.ts";

const NONE: ExportFilterValues = { window: { days: 30 }, tool: "all", project: "all", model: "all" };

// Every page in the nav table (lib/nav.ts, read as source text so this test
// does not load the icon library) has an Export row: an endpoint, or a
// sentence saying there is nothing to export. A page added to the nav without
// a row fails here instead of silently exporting /api/status.
test("every nav page has an export row", () => {
  const src = readFileSync(new URL("./nav.ts", import.meta.url), "utf8");
  const paths = [...src.matchAll(/path: "(\/[^"]*)"/g)].map((m) => m[1]);
  assert.ok(paths.length >= 20, `parsed ${paths.length} nav paths`);
  const covered = new Set(EXPORT_ROUTES.map((r) => r.path));
  const missing = paths.filter((p) => !covered.has(p));
  assert.deepEqual(missing, []);
});

test("no row ever falls back to the status snapshot", () => {
  for (const r of EXPORT_ROUTES) {
    if ("endpoint" in r) assert.notEqual(r.endpoint, "/api/status", r.path);
  }
});

test("an unmapped route says there is nothing to export", () => {
  assert.deepEqual(exportTargetFor("/no-such-page", "", NONE), { none: NO_EXPORT_NOTE });
});

test("a none row carries its own sentence", () => {
  const t = exportTargetFor("/privacy", "", NONE);
  assert.ok("none" in t && t.none.startsWith("Nothing to export here"));
});

test("the export carries the filters its endpoint honours, and only those", () => {
  const f: ExportFilterValues = { window: { days: 7 }, tool: "codex", project: "/p", model: "gpt-5.1" };
  assert.deepEqual(exportTargetFor("/sessions", "", f), {
    url: "/api/sessions?limit=200&days=7&tool=codex&project=%2Fp&model=gpt-5.1",
  });
  // Routing's Decisions tab reads the window and the model, never tool / project.
  assert.deepEqual(exportTargetFor("/routing", "?tab=decisions", f), {
    url: "/api/routing/decisions?limit=1000&days=7&model=gpt-5.1",
  });
  // Unset filters add nothing.
  assert.deepEqual(exportTargetFor("/projects", "", NONE), { url: "/api/projects?days=30" });
  assert.deepEqual(exportTargetFor("/settings", "", f), { url: "/api/config" });
});

test("a tab row wins over its page, with the page's own params", () => {
  const f: ExportFilterValues = { window: { days: 7 }, tool: "codex", project: "/p", model: "gpt-5.1" };
  assert.deepEqual(exportTargetFor("/cost", "?tab=statement&month=2026-09&project=%2Fr", f), {
    url: "/api/report/monthly?month=2026-09&project=%2Fr&model=gpt-5.1",
  });
  assert.deepEqual(exportTargetFor("/cost", "?tab=overview", f), {
    url: "/api/models?days=7&tool=codex&project=%2Fp&model=gpt-5.1",
  });
});

test("search exports its results only once there is a query", () => {
  assert.ok("none" in exportTargetFor("/search", "", NONE));
  assert.deepEqual(exportTargetFor("/search", "?q=deploy", NONE), { url: "/api/search?limit=50&q=deploy" });
});

// REVIEW-3 P3-24: on a tabbed page every tab exports its OWN data or says it
// has nothing to export; no tab falls back to another tab's dataset.
test("every tab of every tabbed page has its own export decision", () => {
  for (const [path, ids] of Object.entries(TABBED_PAGES)) {
    for (const id of ids) {
      const rows = EXPORT_ROUTES.filter((r) => r.path === path && r.tab === id);
      assert.equal(rows.length, 1, `${path} ?tab=${id} needs exactly one row`);
    }
    for (const r of EXPORT_ROUTES.filter((r) => r.path === path)) {
      assert.ok(r.tab != null, `${path}: a tabbed page has no tab-less row (it would answer for every tab)`);
      assert.ok(ids.includes(r.tab), `${path}: row for unknown tab ${r.tab}`);
    }
  }
});

test("the default tab (no ?tab=) exports the first tab's data", () => {
  assert.deepEqual(exportTargetFor("/security", "", NONE), { url: "/api/guard/summary" });
  assert.deepEqual(exportTargetFor("/terminals", "", NONE), { url: "/api/terminal/runs" });
  assert.deepEqual(exportTargetFor("/agent-config", "", NONE), { url: "/api/agent-config/tools" });
});

test("a tab exports what it shows, or says why not", () => {
  assert.deepEqual(exportTargetFor("/security", "?tab=rules", NONE), { url: "/api/guard/rules" });
  assert.deepEqual(exportTargetFor("/security", "?tab=mcp", NONE), { url: "/api/guard/mcp" });
  assert.deepEqual(exportTargetFor("/agent-config", "?tab=coverage", NONE), { url: "/api/agent-config/coverage" });
  const settings = exportTargetFor("/terminals", "?tab=settings", NONE);
  assert.ok("none" in settings && settings.none.startsWith("Nothing to export here"));
});
