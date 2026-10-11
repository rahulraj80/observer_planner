// Pins the pure half of the shared SectionRail (shared/primitives/
// sectionRailModel.ts): which groups render (a group emptied by hidden items
// is dropped, never an orphan heading), the chip-row order, and which section
// a `?section=` value selects (a stale or forbidden deep link lands on a real
// section, never an empty pane), plus LINK rows (`href` / `onSelect`): never a
// section, and only a plain click is intercepted so "open in new tab" works.
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  interceptLinkClick,
  isRailLink,
  pickRailSection,
  railItems,
  shownRailGroups,
} from "../../../shared/primitives/sectionRailModel.ts";

type Sec = "server" | "display" | "sso" | "roles" | "tokens" | "danger";
const GROUPS = [
  { id: "general", label: "General", items: [{ id: "server" as Sec }, { id: "display" as Sec }] },
  { id: "identity", label: "Identity", items: [{ id: "sso" as Sec }, { id: "roles" as Sec, hidden: true }] },
  { id: "data", label: "Data", items: [{ id: "tokens" as Sec, hidden: true }] },
  { id: "zone", items: [{ id: "danger" as Sec }] },
];

test("shownRailGroups drops hidden items and the groups they empty", () => {
  const shown = shownRailGroups(GROUPS);
  assert.deepEqual(
    shown.map((g) => [g.id, g.items.map((i) => i.id)]),
    [
      ["general", ["server", "display"]],
      ["identity", ["sso"]],
      ["zone", ["danger"]],
    ],
  );
  // Group fields other than items are carried through.
  assert.equal(shown[0].label, "General");
});

test("railItems is the shown items in rail order", () => {
  assert.deepEqual(railItems(GROUPS).map((i) => i.id), ["server", "display", "sso", "danger"]);
});

// [?section= value, fallback, want]
const PICK_CASES: Array<[string | null, Sec | undefined, Sec | null]> = [
  ["sso", undefined, "sso"], // cold deep link to a shown section
  ["danger", "server", "danger"],
  [null, undefined, "server"], // no param: first section
  [null, "display", "display"], // no param: the page's default
  ["nonsense", "display", "display"], // stale link: the default
  ["roles", undefined, "server"], // hidden (no permission): first shown
  ["tokens", "display", "display"],
  ["", undefined, "server"],
  [null, "roles", "server"], // a hidden default is not used
];
for (const [raw, fallback, want] of PICK_CASES) {
  test(`pickRailSection(${JSON.stringify(raw)}, ${fallback ?? "-"}) -> ${want}`, () => {
    assert.equal(pickRailSection<Sec>(raw, GROUPS, fallback), want);
  });
}

test("pickRailSection with nothing shown is null", () => {
  assert.equal(pickRailSection("x", [{ id: "g", items: [{ id: "x", hidden: true }] }]), null);
});

// LINK rows (an "Elsewhere" row that leaves the page): never a section.
test("isRailLink: href and/or onSelect make a link row", () => {
  assert.equal(isRailLink({ id: "a" }), false);
  assert.equal(isRailLink({ id: "a", href: "/posture" }), true);
  assert.equal(isRailLink({ id: "a", onSelect: () => {} }), true);
  assert.equal(isRailLink({ id: "a", href: "/posture", onSelect: () => {} }), true);
});

test("pickRailSection never lands on a link row", () => {
  const groups = [
    { id: "elsewhere", items: [{ id: "go-posture", href: "/posture" }] },
    { id: "general", items: [{ id: "server" }, { id: "display" }] },
  ];
  assert.equal(pickRailSection("go-posture", groups), "server", "a link id is not a section");
  assert.equal(pickRailSection(null, groups), "server", "the first SECTION, not the first row");
  assert.equal(pickRailSection(null, groups, "go-posture"), "server", "a link fallback is ignored");
  assert.equal(pickRailSection(null, [{ id: "g", items: [{ id: "x", onSelect: () => {} }] }]), null);
});

// [name, event, intercepted]
const CLICK = { button: 0, metaKey: false, ctrlKey: false, shiftKey: false, altKey: false };
const CLICK_CASES: Array<[string, Partial<typeof CLICK> & { defaultPrevented?: boolean }, boolean]> = [
  ["plain left click runs the in-app action", {}, true],
  ["middle click opens a new tab", { button: 1 }, false],
  ["Ctrl-click opens a new tab", { ctrlKey: true }, false],
  ["Cmd-click opens a new tab", { metaKey: true }, false],
  ["Shift-click opens a new window", { shiftKey: true }, false],
  ["Alt-click downloads", { altKey: true }, false],
  ["a click something else already handled", { defaultPrevented: true }, false],
];
for (const [name, e, want] of CLICK_CASES) {
  test(`interceptLinkClick: ${name}`, () => {
    assert.equal(interceptLinkClick({ ...CLICK, ...e }), want);
  });
}
