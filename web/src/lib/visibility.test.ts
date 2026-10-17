import { test } from "node:test";
import assert from "node:assert/strict";
import {
  FLOOR_NAV_IDS,
  FLOOR_SETTINGS_IDS,
  failClosedLists,
  governancePosture,
  helpEntryHidden,
  settingsOfferedWhileUnresolved,
  hiddenNavIds,
  hiddenSettingsIds,
  navSource,
  orgDisplayLabel,
  orgDisplayLabelSentence,
  pathNavId,
  readOnlyNavIds,
  readOnlySettingsIds,
  withoutHiddenPaths,
  type GovernanceLists,
} from "./visibility.ts";
import { orgDisplayLabel as ownerLabel, orgDisplayLabelSentence as ownerSentence } from "./orgName.ts";

const NAV = [
  { id: "overview", path: "/" },
  { id: "sessions", path: "/sessions" },
  { id: "security", path: "/security" },
  { id: "routing", path: "/routing" },
  { id: "compression", path: "/compression" },
];

const solo: GovernanceLists = {
  active: false,
  hidden_sections: [],
  read_only_sections: [],
  hidden_settings: [],
  read_only_settings: [],
  visibility: {
    managed_defaults: false,
    hidden_sections: [],
    read_only_sections: [],
    hidden_settings: [],
    read_only_settings: [],
  },
};

const managed: GovernanceLists = {
  active: false, // managed defaults apply with no body published
  hidden_sections: [],
  read_only_sections: [],
  hidden_settings: [],
  read_only_settings: [],
  visibility: {
    managed_defaults: true,
    hidden_sections: ["security", "routing"],
    read_only_sections: ["compression"],
    hidden_settings: ["guard"],
    read_only_settings: ["proxy"],
    sections: {
      security: { level: "hidden", source: "default" },
      routing: { level: "hidden", source: "org" },
    },
  },
};

test("an individual node hides nothing", () => {
  assert.deepEqual([...hiddenNavIds(solo)], []);
  assert.deepEqual([...readOnlySettingsIds(solo)], []);
  assert.deepEqual([...hiddenNavIds(null)], []);
  assert.deepEqual([...hiddenNavIds(undefined)], []);
});

test("a managed node reads the resolved visibility, even with no body applied", () => {
  assert.deepEqual([...hiddenNavIds(managed)], ["security", "routing"]);
  assert.deepEqual([...readOnlyNavIds(managed)], ["compression"]);
  assert.deepEqual([...hiddenSettingsIds(managed)], ["guard"]);
  assert.deepEqual([...readOnlySettingsIds(managed)], ["proxy"]);
});

test("a pre-arc daemon (no visibility block) falls back to the active legacy lists", () => {
  const legacy: GovernanceLists = {
    active: true,
    hidden_sections: ["benchmarks"],
    read_only_sections: [],
    hidden_settings: [],
    read_only_settings: ["guard"],
  };
  assert.deepEqual([...hiddenNavIds(legacy)], ["benchmarks"]);
  assert.deepEqual([...readOnlySettingsIds(legacy)], ["guard"]);
  assert.deepEqual([...hiddenNavIds({ ...legacy, active: false })], []);
});

test("navSource names what decided a page", () => {
  assert.equal(navSource(managed, "security"), "default");
  assert.equal(navSource(managed, "routing"), "org");
  assert.equal(navSource(managed, "sessions"), "none");
  assert.equal(navSource(null, "security"), "none");
});

test("pathNavId maps paths with queries onto nav ids", () => {
  assert.equal(pathNavId("/security?tab=rules", NAV), "security");
  assert.equal(pathNavId("/", NAV), "overview");
  assert.equal(pathNavId("/nowhere", NAV), null);
});

test("withoutHiddenPaths drops palette jumps and tour steps to hidden pages", () => {
  const items = [
    { path: "/" },
    { path: "/security" },
    { path: "/security?tab=rules" },
    { path: "/sessions" },
    { path: undefined },
    { path: "/unknown" },
  ];
  const kept = withoutHiddenPaths(items, (i) => i.path, hiddenNavIds(managed), NAV);
  assert.deepEqual(
    kept.map((i) => i.path),
    ["/", "/sessions", undefined, "/unknown"],
  );
  assert.equal(withoutHiddenPaths(items, (i) => i.path, [], NAV).length, items.length);
});

test("helpEntryHidden matches page-, page-prefixed and settings-prefixed ids", () => {
  const nav = ["security", "routing"];
  const settings = ["guard"];
  for (const id of ["tab.security", "card.security_mode", "chart.routing_decisions", "glossary.settings_guard"]) {
    assert.equal(helpEntryHidden(id, nav, settings), true, id);
  }
  for (const id of ["tab.sessions", "card.privacy_org_push", "glossary.securityish", "glossary.settings_routing", "nodot"]) {
    assert.equal(helpEntryHidden(id, nav, settings), false, id);
  }
});

// Sol A4-05: a posture that has not resolved (or failed to) must not read as
// an ungoverned node. Only an answer is "ready"; a later refresh error keeps
// the last answer, which is what the daemon still enforces.
test("governancePosture: only an answer is ready", () => {
  assert.equal(governancePosture({ data: null, error: null }), "pending");
  assert.equal(governancePosture({ data: null, error: new Error("502") }), "unavailable");
  assert.equal(governancePosture({ data: solo, error: null }), "ready");
  assert.equal(governancePosture({ data: managed, error: new Error("refresh failed") }), "ready");
});

test("failClosedLists hides every page but the floor until the posture is known", () => {
  const ids = ["overview", "sessions", "security", "routing", "settings", "privacy"];
  const closed = failClosedLists(ids);
  assert.deepEqual([...hiddenNavIds(closed)], ["overview", "sessions", "security", "routing"]);
  for (const floor of FLOOR_NAV_IDS) assert.ok(!hiddenNavIds(closed).includes(floor), floor);
  assert.deepEqual([...readOnlyNavIds(closed)], []);
  // A palette jump or tour step to a governed page is dropped; Privacy stays.
  const nav = [...NAV, { id: "privacy", path: "/privacy" }];
  const kept = withoutHiddenPaths([{ p: "/security" }, { p: "/" }, { p: "/privacy" }], (i) => i.p, hiddenNavIds(closed), nav);
  assert.deepEqual(kept.map((i) => i.p), ["/privacy"]);
});

test("Settings and help fail closed to the floor sections while unresolved", () => {
  for (const id of FLOOR_SETTINGS_IDS) assert.equal(settingsOfferedWhileUnresolved(id), true, id);
  for (const id of ["guard", "pricing", "storage", "tools"]) assert.equal(settingsOfferedWhileUnresolved(id), false, id);
  assert.equal(helpEntryHidden("glossary.settings_guard", [], [], true), true);
  assert.equal(helpEntryHidden("glossary.settings_storage_vacuum", [], [], true), true);
  assert.equal(helpEntryHidden("glossary.settings_enrolment", [], [], true), false);
  assert.equal(helpEntryHidden("card.settings_org_share", [], [], true), false);
  assert.equal(helpEntryHidden("glossary.settings_guard", [], [], false), false);
});

// The org display name moved to ./orgName (orgName.test.ts owns its rows);
// this pins only that the governance surfaces' re-export is the same function.
test("visibility re-exports the one org display-name helper", () => {
  assert.equal(orgDisplayLabel, ownerLabel);
  assert.equal(orgDisplayLabelSentence, ownerSentence);
});
