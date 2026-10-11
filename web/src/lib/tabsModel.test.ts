// Pins the pure half of the platform tab pattern (docs/app-design-system.md
// "Page tabs"): the TabStrip's WAI-ARIA keyboard navigation over usable tabs
// (hidden and disabled are skipped), the roving-tabindex choice, resolveTab
// for a deep link to a hidden or disabled tab, the audited marker's copy, the
// keep-alive TabPanel latch (a panel's loader runs once across any number of
// switches and never before its tab is opened), and the strip's
// scroll-into-view / edge-fade geometry.
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  AUDITED_SR_SUFFIX,
  auditedNote,
  focusableTabId,
  latchVisited,
  nextTabId,
  resolveTab,
  shownTabs,
  tabPanelState,
  usableTabs,
  type TabRuleInput,
} from "../../../shared/primitives/tabsModel.ts";
import { readFileSync } from "node:fs";
import {
  EDGE_FADE_PX,
  contentOverflows,
  edgeFadeMask,
  overflowEdges,
  revealScrollLeft,
  stripRevealMode,
  type StripLayoutEvent,
} from "../../../shared/lib/hscroll.ts";

type Id = "overview" | "sessions" | "efficiency" | "terminals" | "security";
const TABS: TabRuleInput<Id>[] = [
  { id: "overview" },
  { id: "sessions" },
  { id: "efficiency", disabled: true },
  { id: "terminals", hidden: true },
  { id: "security" },
];

test("shownTabs drops hidden tabs; usableTabs also drops disabled ones", () => {
  assert.deepEqual(shownTabs(TABS).map((t) => t.id), ["overview", "sessions", "efficiency", "security"]);
  assert.deepEqual(usableTabs(TABS).map((t) => t.id), ["overview", "sessions", "security"]);
});

// [current, key, want] - one row per WAI-ARIA rule.
const KEY_CASES: Array<[Id | null, string, Id | null]> = [
  ["overview", "ArrowRight", "sessions"],
  ["sessions", "ArrowRight", "security"], // skips disabled + hidden
  ["security", "ArrowRight", "overview"], // wraps
  ["overview", "ArrowLeft", "security"], // wraps backwards
  ["security", "ArrowLeft", "sessions"],
  ["sessions", "Home", "overview"],
  ["overview", "End", "security"],
  ["efficiency", "ArrowRight", "overview"], // current not usable: first
  ["terminals", "ArrowLeft", "security"], // current not usable: last
  [null, "ArrowRight", "overview"],
  ["overview", "ArrowDown", null], // not a horizontal tablist key
  ["overview", "Enter", null],
  ["overview", "a", null],
];
for (const [current, key, want] of KEY_CASES) {
  test(`nextTabId(${current}, ${key}) -> ${want}`, () => {
    assert.equal(nextTabId(TABS, current, key), want);
  });
}

test("nextTabId has nowhere to go when no tab is usable", () => {
  const none: TabRuleInput[] = [{ id: "a", disabled: true }, { id: "b", hidden: true }];
  assert.equal(nextTabId(none, "a", "ArrowRight"), null);
  assert.equal(focusableTabId(none, "a"), null);
});

test("focusableTabId keeps the strip reachable: the value, else the first usable tab", () => {
  assert.equal(focusableTabId(TABS, "security"), "security");
  assert.equal(focusableTabId(TABS, "efficiency"), "overview"); // disabled value
  assert.equal(focusableTabId(TABS, "terminals"), "overview"); // hidden value
});

test("resolveTab: a deep link to a hidden or disabled tab lands on the first usable one", () => {
  assert.equal(resolveTab("sessions", TABS), "sessions");
  assert.equal(resolveTab("terminals", TABS), "overview");
  assert.equal(resolveTab("efficiency", TABS), "overview");
  assert.equal(resolveTab(null, TABS), "overview");
  const firstHidden: TabRuleInput[] = [{ id: "a", hidden: true }, { id: "b" }];
  assert.equal(resolveTab("a", firstHidden), "b");
});

test("auditedNote names the audit actions, deduplicated, in order", () => {
  assert.equal(
    auditedNote(["view_guard_posture", "view_guard_pins", "view_guard_posture"]),
    "Opening this tab is recorded in the audit log (view_guard_posture, view_guard_pins).",
  );
  assert.equal(auditedNote([]), "Opening this tab is recorded in the audit log.");
  assert.equal(auditedNote([" ", ""]), "Opening this tab is recorded in the audit log.");
  assert.equal(AUDITED_SR_SUFFIX, "(audited)");
});

test("auditedNote copy carries no em dash", () => {
  assert.ok(!auditedNote(["view_x"]).includes("—"));
});

// Drive the TabPanel decision functions through a click sequence and count
// how often each panel MOUNTS (its loaders run on mount). This is the
// property the audit trail depends on: one audited read per page visit,
// however often the viewer switches tabs, and none for a tab never opened.
function simulate(ids: readonly Id[], clicks: readonly Id[]) {
  const visited = new Map<Id, boolean>(ids.map((id) => [id, false]));
  const mounted = new Map<Id, boolean>(ids.map((id) => [id, false]));
  const mounts = new Map<Id, number>(ids.map((id) => [id, 0]));
  const states: string[] = [];
  for (const active of clicks) {
    for (const id of ids) {
      const v = latchVisited(visited.get(id)!, id === active);
      visited.set(id, v);
      const s = tabPanelState(id, active, v);
      const isMounted = s !== "unmounted";
      if (isMounted && !mounted.get(id)) mounts.set(id, mounts.get(id)! + 1);
      if (!isMounted && mounted.get(id)) throw new Error(`${id} unmounted after mounting`);
      mounted.set(id, isMounted);
      if (id === "security") states.push(s);
    }
  }
  return { mounts, states };
}

test("keep-alive: a panel's loader runs once across repeated switches", () => {
  const ids: Id[] = ["overview", "sessions", "security"];
  const { mounts, states } = simulate(ids, [
    "overview",
    "sessions",
    "overview",
    "security",
    "overview",
    "security",
    "sessions",
    "security",
  ]);
  assert.equal(mounts.get("overview"), 1);
  assert.equal(mounts.get("sessions"), 1);
  assert.equal(mounts.get("security"), 1);
  // Not mounted before its first activation, hidden (still mounted) after.
  assert.deepEqual(states, [
    "unmounted",
    "unmounted",
    "unmounted",
    "active",
    "hidden",
    "active",
    "hidden",
    "active",
  ]);
});

test("keep-alive: an audited tab never opened is never mounted (no audited read)", () => {
  const ids: Id[] = ["overview", "sessions", "security"];
  const { mounts } = simulate(ids, ["overview", "sessions", "overview", "sessions"]);
  assert.equal(mounts.get("security"), 0);
});

test("keep-alive: a cold deep link mounts only the linked tab", () => {
  const ids: Id[] = ["overview", "sessions", "security"];
  const { mounts } = simulate(ids, ["security"]);
  assert.deepEqual([...mounts.values()], [0, 0, 1]);
});

test("latchVisited is monotonic", () => {
  assert.equal(latchVisited(false, false), false);
  assert.equal(latchVisited(false, true), true);
  assert.equal(latchVisited(true, false), true);
});

// revealScrollLeft: a 375 px phone strip, 800 px of tabs, 100 px tabs.
const strip = { clientWidth: 375, scrollWidth: 800, inset: EDGE_FADE_PX };
const REVEAL_CASES: Array<[string, number, number, number, number | null]> = [
  // [name, scrollLeft, itemLeft, itemWidth, want]
  ["visible item does not move the strip", 0, 100, 100, null],
  ["6th tab off to the right lands at the right edge", 0, 500, 100, 500 + 100 - 375 + EDGE_FADE_PX],
  ["last tab clamps to the end", 0, 700, 100, 800 - 375],
  ["item off to the left lands at the left edge", 400, 200, 100, 200 - EDGE_FADE_PX],
  ["first tab clamps to 0", 300, 0, 100, 0],
  ["item under the right fade is pulled clear", 0, 260, 100, 260 + 100 - 375 + EDGE_FADE_PX],
];
for (const [name, scrollLeft, itemLeft, itemWidth, want] of REVEAL_CASES) {
  test(`revealScrollLeft: ${name}`, () => {
    assert.equal(revealScrollLeft({ ...strip, scrollLeft, itemLeft, itemWidth }), want);
  });
}

test("revealScrollLeft: a strip that does not overflow never scrolls", () => {
  assert.equal(
    revealScrollLeft({ scrollLeft: 0, clientWidth: 1200, scrollWidth: 1200, itemLeft: 900, itemWidth: 100 }),
    null,
  );
});

test("revealScrollLeft: an item wider than the view aligns its start", () => {
  assert.equal(
    revealScrollLeft({ scrollLeft: 0, clientWidth: 300, scrollWidth: 900, itemLeft: 400, itemWidth: 320 }),
    400 - EDGE_FADE_PX,
  );
});

// The last tab of an overflowing strip lands at the very end, not one flex
// gap short of it: stopping short left the end fade drawn over its trailing
// count ("Shared summaries 0 of 3" on Control Center at 375, measured: the
// strip stopped at 79 of a max 81 and still faded its end).
test("revealScrollLeft: a target within a fade width of the end snaps to the end", () => {
  const cc = { clientWidth: 351, scrollWidth: 432, inset: EDGE_FADE_PX };
  assert.equal(revealScrollLeft({ ...cc, scrollLeft: 0, itemLeft: 207, itemWidth: 199 }), 81);
  // ... and at the end there is no end fade left to draw.
  assert.deepEqual(overflowEdges({ scrollLeft: 81, clientWidth: 351, scrollWidth: 432 }), { start: true, end: false });
});

test("revealScrollLeft: a target within a fade width of the start snaps to 0", () => {
  assert.equal(
    revealScrollLeft({ scrollLeft: 200, clientWidth: 375, scrollWidth: 800, itemLeft: 40, itemWidth: 100 }),
    0,
  );
});

test("overflowEdges and edgeFadeMask fade only the ends that hide content", () => {
  const none = overflowEdges({ scrollLeft: 0, clientWidth: 500, scrollWidth: 500 });
  assert.deepEqual(none, { start: false, end: false });
  assert.equal(edgeFadeMask(none), undefined);

  const atStart = overflowEdges({ scrollLeft: 0, clientWidth: 375, scrollWidth: 800 });
  assert.deepEqual(atStart, { start: false, end: true });
  assert.equal(
    edgeFadeMask(atStart),
    `linear-gradient(to right, #000 0, #000 calc(100% - ${EDGE_FADE_PX}px), transparent 100%)`,
  );

  const middle = overflowEdges({ scrollLeft: 200, clientWidth: 375, scrollWidth: 800 });
  assert.deepEqual(middle, { start: true, end: true });

  const atEnd = overflowEdges({ scrollLeft: 425, clientWidth: 375, scrollWidth: 800 });
  assert.deepEqual(atEnd, { start: true, end: false });
  assert.equal(
    edgeFadeMask(atEnd),
    `linear-gradient(to right, transparent 0, #000 ${EDGE_FADE_PX}px, #000 100%)`,
  );

  // Sub-pixel scroll positions do not flicker a fade on.
  assert.deepEqual(overflowEdges({ scrollLeft: 0.5, clientWidth: 375, scrollWidth: 375.6 }), {
    start: false,
    end: false,
  });
});

// stripRevealMode: which layout events scroll the active item back into view.
// [event, userScrolled, reducedMotion, smoothInFlight, want]
const MODE_CASES: Array<[StripLayoutEvent, boolean, boolean, boolean, "instant" | "smooth" | null]> = [
  ["mount", false, false, false, "instant"], // a cold deep link opens on its tab
  ["mount", true, false, false, "instant"],
  ["active-change", false, false, false, "smooth"],
  ["active-change", true, false, false, "smooth"], // picking a tab ends the hold
  ["active-change", false, true, false, "instant"], // reduced motion
  ["shown", false, false, false, "instant"], // chip row appearing below its breakpoint
  ["shown", true, false, false, "instant"],
  ["resize", false, false, false, "instant"], // a count / label suffix arrived late
  ["resize", true, false, false, null], // the user's hand scroll wins
  ["resize", false, false, true, "smooth"], // the active tab turning bold mid-animation
  ["resize", false, true, true, "instant"],
];
for (const [event, userScrolled, reducedMotion, smoothInFlight, want] of MODE_CASES) {
  test(`stripRevealMode(${event}, scrolled=${userScrolled}, rm=${reducedMotion}, inflight=${smoothInFlight}) -> ${want}`, () => {
    assert.equal(stripRevealMode(event, { userScrolled, reducedMotion, smoothInFlight }), want);
  });
}

// contentOverflows: the end pad is measured OUT, so it never keeps itself on.
const OVERFLOW_CASES: Array<[string, number, number, number, boolean]> = [
  // [name, scrollWidth, clientWidth, padWidth, want]
  ["fits", 360, 375, 0, false],
  ["overflows", 420, 375, 0, true],
  ["overflows with the pad rendered", 444, 375, EDGE_FADE_PX, true],
  ["the pad alone does not count as overflow", 380, 375, EDGE_FADE_PX, false],
  ["sub-pixel tolerance", 375.8, 375, 0, false],
];
for (const [name, scrollWidth, clientWidth, padWidth, want] of OVERFLOW_CASES) {
  test(`contentOverflows: ${name}`, () => {
    assert.equal(contentOverflows({ scrollWidth, clientWidth, padWidth }), want);
  });
}

// The reported 375 px bug (Security ?tab=control-coverage, Control Center
// "0 of 3"): the last tab is revealed on mount, then a count renders in an
// earlier tab and pushes the active one right, under the fade. Re-running the
// reveal on that resize, with the end pad in place, brings it fully clear.
test("a late count pushing the active last tab under the fade is re-revealed clear of it", () => {
  const clientWidth = 375;
  // Mount: Overview 0-100, Agent chains 100-260, Control coverage 260-420.
  const mount = revealScrollLeft({ scrollLeft: 0, clientWidth, scrollWidth: 420, itemLeft: 260, itemWidth: 160 });
  assert.equal(mount, 45);
  // The "3 *" count lands in Agent chains (+30 px): the active tab now ends
  // at 450 while the strip still shows up to 45 + 375 = 420.
  const itemLeft = 290;
  const itemWidth = 160;
  assert.ok(itemLeft + itemWidth > mount! + clientWidth, "clipped without a re-reveal");
  // Re-reveal with the end pad rendered (scrollWidth grows by EDGE_FADE_PX).
  const scrollWidth = 450 + EDGE_FADE_PX;
  const again = revealScrollLeft({ scrollLeft: mount!, clientWidth, scrollWidth, itemLeft, itemWidth });
  assert.equal(again, scrollWidth - clientWidth, "scrolled to the end");
  // The whole tab, label suffix included, sits left of where the fade starts.
  assert.ok(itemLeft + itemWidth - again! <= clientWidth - EDGE_FADE_PX);
  assert.deepEqual(overflowEdges({ scrollLeft: again!, clientWidth, scrollWidth }), { start: true, end: false });
});

// The hook's wiring cannot run in node (no layout engine); pin that a resize
// goes through the table above and that later-added items are observed.
test("useStripScroll routes strip/content resizes through stripRevealMode and renders the end pad", () => {
  const src = readFileSync(new URL("../../../shared/primitives/useStripScroll.ts", import.meta.url), "utf8");
  assert.match(src, /stripRevealMode\(event, \{/);
  assert.match(src, /reveal\(event\);\s*measure\(\);/);
  assert.match(src, /new MutationObserver/);
  assert.match(src, /observer\.observe\(n\)/);
  for (const strip of ["Tabs.tsx", "SectionRail.tsx"]) {
    const s = readFileSync(new URL(`../../../shared/primitives/${strip}`, import.meta.url), "utf8");
    assert.match(s, /\{endPad && <StripEndPad \/>\}/, strip);
  }
});
