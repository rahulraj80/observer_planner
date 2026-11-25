import { test } from "node:test";
import assert from "node:assert/strict";
import { METRIC_ICONS } from "../../../shared/lib/metricIcons.ts";
import { APPS_METRIC_ICONS } from "./appsMetricIcons.ts";
import { web2Skip } from "./web2Absent.ts";

// The AI apps tiles' glyphs: no shadowing of a shared key, no glyph that
// already means another metric, and the org's glyph wherever the org draws
// the same metric (one metric, one glyph, on both dashboards).

test("no AI apps metric shadows a shared metric key", () => {
  for (const key of Object.keys(APPS_METRIC_ICONS)) {
    assert.ok(!(key in METRIC_ICONS), `${key} shadows METRIC_ICONS`);
  }
});

test("AI apps metrics have distinct glyphs, unused by the shared table", () => {
  const shared = new Set<unknown>(Object.values(METRIC_ICONS));
  const seen = new Map<unknown, string>();
  for (const [key, icon] of Object.entries(APPS_METRIC_ICONS)) {
    assert.ok(!shared.has(icon), `${key} reuses a shared metric glyph`);
    assert.equal(seen.get(icon), undefined, `${key} reuses the glyph of ${seen.get(icon)}`);
    seen.set(icon, key);
  }
});

// Fork change: web2 is loaded at run time (a computed specifier tsc does not
// resolve), so the public snapshot, which lacks web2/, skips this test.
const ORG_ICONS = "../../../web2/src/lib/orgMetricIcons.ts";
test("an AI apps metric the org also draws uses the org's glyph", { skip: web2Skip("web2/src/lib/orgMetricIcons.ts") }, async () => {
  const { ORG_METRIC_ICONS } = (await import(ORG_ICONS)) as { ORG_METRIC_ICONS: Record<string, unknown> };
  const org = ORG_METRIC_ICONS;
  const orgGlyphs = new Map<unknown, string>(Object.entries(org).map(([k, v]) => [v, k]));
  for (const [key, icon] of Object.entries(APPS_METRIC_ICONS)) {
    if (key in org) assert.equal(icon, org[key], `${key} differs from the org's glyph`);
    else assert.equal(orgGlyphs.get(icon), undefined, `${key} reuses the org glyph of ${orgGlyphs.get(icon)}`);
  }
});
