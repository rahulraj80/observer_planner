import { test } from "node:test";
import assert from "node:assert/strict";
import { NAV_GROUPS, NAV_ITEMS, type NavGroup } from "./nav.ts";
import { TOUR_STEPS } from "./tour.ts";
import {
  NAV_ADVANCED,
  NAV_FOOT_IDS,
  NAV_MAIN_IDS,
  advancedGroupOf,
  layoutNav,
  navCrumb,
  parsePins,
} from "./navLayout.ts";

const ids = (items: { id: string }[]) => items.map((i) => i.id);
const shown = (l: ReturnType<typeof layoutNav>) => l.sections.flatMap((s) => ids(s.items));

test("every page is in the sidebar exactly once when Advanced is unfolded", () => {
  const l = layoutNav(NAV_GROUPS, { open: true });
  assert.deepEqual([...shown(l)].sort(), ids(NAV_ITEMS).sort());
});

test("the layout names only pages the nav has, each once", () => {
  const named = [...NAV_MAIN_IDS, ...NAV_FOOT_IDS, ...NAV_ADVANCED.flatMap((s) => s.items)];
  assert.equal(new Set(named).size, named.length, "a page is placed twice");
  const nav = new Set(ids(NAV_ITEMS));
  for (const id of named) assert.ok(nav.has(id), `${id} is not a nav item`);
});

test("folded, the sidebar is the main list, the Advanced heading and the bottom strip", () => {
  const l = layoutNav(NAV_GROUPS, { pathname: "/" });
  assert.deepEqual(
    l.sections.map((s) => [s.tier, ids(s.items), !!s.toggle]),
    [
      ["main", [...NAV_MAIN_IDS], false],
      ["advanced", [], true],
      ["foot", [...NAV_FOOT_IDS], false],
    ],
  );
  assert.equal(l.folded, NAV_ITEMS.length - NAV_MAIN_IDS.length - NAV_FOOT_IDS.length);
  assert.ok(NAV_MAIN_IDS.length <= 8, "the main list stays short");
});

test("unfolded, Advanced is its sub-groups in order, the heading above the first", () => {
  const l = layoutNav(NAV_GROUPS, { open: true });
  const adv = l.sections.filter((s) => s.tier === "advanced");
  assert.deepEqual(adv.map((s) => s.label), NAV_ADVANCED.map((s) => s.label));
  assert.deepEqual(adv.map((s) => !!s.toggle), [true, false, false]);
  for (const [i, s] of adv.entries()) assert.deepEqual(ids(s.items), NAV_ADVANCED[i].items);
});

test("folded, the page being viewed still shows under the Advanced heading", () => {
  const l = layoutNav(NAV_GROUPS, { pathname: "/cache" });
  const adv = l.sections.filter((s) => s.tier === "advanced");
  assert.equal(adv.length, 1);
  assert.deepEqual(ids(adv[0].items), ["cache"]);
  assert.equal(adv[0].toggle, true);
});

// The first-run tour points at sidebar rows; it walks to each step's page
// first, so with Advanced folded every anchored row must be in the sidebar
// while its page is the current one.
test("every tour step's sidebar row is listed when the tour is on its page", () => {
  let anchored = 0;
  for (const step of TOUR_STEPS) {
    const m = /^\[data-tour="nav-([a-z0-9-]+)"\]$/.exec(step.target ?? "");
    if (!m) continue;
    anchored++;
    const l = layoutNav(NAV_GROUPS, { pathname: step.path });
    assert.ok(shown(l).includes(m[1]), `tour step ${step.id}: nav-${m[1]} is folded away on ${step.path}`);
  }
  assert.ok(anchored >= 5, "the tour anchors to sidebar rows");
});

test("a kept page moves to the main list and out of Advanced", () => {
  const l = layoutNav(NAV_GROUPS, { pins: ["cache", "plans"], open: true });
  const main = l.sections.find((s) => s.tier === "main")!;
  // After the fixed main pages, in Advanced order (not the order kept).
  assert.deepEqual(ids(main.items), [...NAV_MAIN_IDS, "plans", "cache"]);
  const adv = l.sections.filter((s) => s.tier === "advanced").flatMap((s) => ids(s.items));
  assert.ok(!adv.includes("cache") && !adv.includes("plans"));
  assert.deepEqual(
    l.choices.filter((c) => c.pinned).map((c) => c.item.id),
    ["plans", "cache"],
  );
  assert.equal(l.choices.length, l.folded + 2);
});

test("keeping a main page, a bottom page or an unknown id changes nothing", () => {
  const base = layoutNav(NAV_GROUPS, { open: true });
  const l = layoutNav(NAV_GROUPS, { pins: ["overview", "settings", "no-such-page"], open: true });
  assert.deepEqual(
    l.sections.map((s) => ids(s.items)),
    base.sections.map((s) => ids(s.items)),
  );
});

test("with every Advanced page kept, the Advanced heading is gone", () => {
  const all = layoutNav(NAV_GROUPS).choices.map((c) => c.item.id);
  const l = layoutNav(NAV_GROUPS, { pins: all });
  assert.equal(l.folded, 0);
  assert.ok(!l.sections.some((s) => s.tier === "advanced"));
});

test("a page the layout does not name lands in Advanced by its nav group", () => {
  const icon = NAV_ITEMS[0].icon;
  const groups: NavGroup[] = [
    { id: "monitor", label: "Monitor", items: [{ id: "overview", label: "Overview", path: "/", icon }] },
    { id: "optimize", label: "Optimize", items: [{ id: "newtuner", label: "New tuner", path: "/newtuner", icon }] },
    { id: "configure", label: "Configure", items: [{ id: "newknob", label: "New knob", path: "/newknob", icon }] },
    { id: "brand-new", label: "Brand new", items: [{ id: "newthing", label: "New thing", path: "/newthing", icon }] },
  ];
  assert.equal(advancedGroupOf("newtuner", "optimize"), "optimize");
  assert.equal(advancedGroupOf("newknob", "configure"), "control");
  assert.equal(advancedGroupOf("newthing", "brand-new"), "insights");
  assert.equal(advancedGroupOf("overview", "monitor"), null);
  const l = layoutNav(groups, { open: true });
  assert.deepEqual(
    l.sections.map((s) => [s.tier, s.label, ids(s.items)]),
    [
      ["main", "", ["overview"]],
      ["advanced", "Insights", ["newthing"]],
      ["advanced", "Optimize", ["newtuner"]],
      ["advanced", "Control", ["newknob"]],
    ],
  );
});

// A governed machine may be left with only a few pages (lib/governance.ts);
// the layout shows what there is and no empty heading.
test("a filtered nav lays out what is left", () => {
  const only = (keep: string[]) =>
    NAV_GROUPS.map((g) => ({ ...g, items: g.items.filter((i) => keep.includes(i.id)) })).filter(
      (g) => g.items.length > 0,
    );
  const l = layoutNav(only(["overview", "privacy", "settings"]), { open: true });
  assert.deepEqual(
    l.sections.map((s) => [s.tier, ids(s.items), !!s.toggle]),
    [
      ["main", ["overview"], false],
      ["foot", ["privacy", "settings"], false],
    ],
  );
  assert.deepEqual(layoutNav([]).sections, []);
});

test("the breadcrumb names an Advanced page's sub-group and nothing for the rest", () => {
  const cases: { path: string; label: string | null; id?: string }[] = [
    { path: "/", label: "", id: "overview" },
    { path: "/settings", label: "", id: "settings" },
    { path: "/cache", label: "Optimize", id: "cache" },
    { path: "/analysis", label: "Insights", id: "analysis" },
    { path: "/egress", label: "Control", id: "egress" },
    { path: "/no-such-page", label: null },
  ];
  for (const c of cases) {
    const got = navCrumb(NAV_GROUPS, c.path);
    assert.equal(got?.label ?? null, c.label, c.path);
    assert.equal(got?.item.id, c.id, c.path);
  }
});

test("a stored list of kept pages is read defensively", () => {
  const cases: { raw: string | null; want: string[] }[] = [
    { raw: null, want: [] },
    { raw: "", want: [] },
    { raw: "not json", want: [] },
    { raw: '{"a":1}', want: [] },
    { raw: '["cache","plans","cache"]', want: ["cache", "plans"] },
    { raw: '["cache",7,null,"<script>","UPPER",""]', want: ["cache"] },
  ];
  for (const c of cases) assert.deepEqual(parsePins(c.raw), c.want, String(c.raw));
  assert.equal(parsePins(JSON.stringify(Array.from({ length: 100 }, (_, i) => `p${i}`))).length, 40);
});
