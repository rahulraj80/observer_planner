import { test } from "node:test";
import assert from "node:assert/strict";
import {
  ARTIFACT_STATE_LABEL,
  absentSettingsSections,
  artifactWarns,
  badgeFor,
  distrosWith,
  filterNavByComponents,
  groupOf,
  inactiveNavIds,
  isComponentActive,
  isRouteInactive,
  manifestFor,
  parseNotInstalled,
  setInactiveRoutes,
  stateCopy,
  tileVisible,
  type CatalogComponent,
  type ComponentsView,
} from "./componentState.ts";

const view: ComponentsView = {
  configured: true,
  base: "developer",
  add: [],
  remove: ["terminals"],
  binary_distro: "full",
  components_digest: "abc",
  compiled_count: 3,
  active_count: 1,
  components: [
    { id: "core", state: "active", reason: "core", compiled: true, selected: true, in_base: true },
    { id: "terminals", state: "available", reason: "not_selected", compiled: true, selected: false, in_base: true },
    { id: "codeintel", state: "absent", reason: "not_compiled", compiled: false, selected: true, in_base: true },
  ],
  issues: [],
  restart_note: "",
};

test("badgeFor walks its rules in order", () => {
  assert.equal(badgeFor({ id: "codeintel", state: "absent" }, [{ id: "codeintel", change: "deactivate" }]), "not_in_build");
  assert.equal(badgeFor({ id: "x", state: "blocked" }), "blocked");
  assert.equal(badgeFor({ id: "t", state: "available" }, [{ id: "t", change: "deactivate" }]), "removal_pending");
  assert.equal(badgeFor({ id: "t", state: "active" }, [{ id: "t", change: "activate" }]), "activation_pending");
  assert.equal(badgeFor({ id: "t", state: "active" }), "active");
  assert.equal(badgeFor({ id: "t", state: "available" }), "inactive");
});

test("isComponentActive fails open", () => {
  assert.equal(isComponentActive(null, "terminals"), true);
  assert.equal(isComponentActive(view, "core"), true);
  assert.equal(isComponentActive(view, "unknown.row"), true);
  assert.equal(isComponentActive(view, "terminals"), false);
  assert.equal(isComponentActive(view, "codeintel"), false);
});

test("Settings hides only the sections of components not in this build", () => {
  assert.deepEqual(absentSettingsSections(null), []);
  const v: ComponentsView = {
    ...view,
    components: [
      { id: "obs", state: "absent", reason: "not_compiled", compiled: false, selected: false, in_base: false },
      { id: "processobs", state: "available", reason: "not_selected", compiled: true, selected: false, in_base: false },
      { id: "export.otel", state: "absent", reason: "not_compiled", compiled: false, selected: false, in_base: false },
    ],
  };
  // processobs is installed but inactive (L2): its section stays so it can
  // be set up before it is added.
  assert.deepEqual(absentSettingsSections(v), ["observability", "otel"]);
  assert.deepEqual(absentSettingsSections(view), []);
});

test("nav filter drops the pages of inactive components only", () => {
  const nav = { overview: "core", terminals: "terminals", settings: "core" };
  const ids = inactiveNavIds(view, nav);
  assert.deepEqual(ids, ["terminals"]);
  const groups = [
    { id: "a", items: [{ id: "overview" }, { id: "terminals" }] },
    { id: "b", items: [{ id: "terminals" }] },
  ];
  const out = filterNavByComponents(groups, ids);
  assert.deepEqual(out.map((g) => g.items.map((i) => i.id)), [["overview"]]);
  assert.equal(filterNavByComponents(groups, []), groups, "identity when nothing is inactive");
  assert.deepEqual(inactiveNavIds(null, nav), []);
});

test("parseNotInstalled reads only the component_not_installed class", () => {
  const body = JSON.stringify({ error: "component_not_installed", component: "terminals", state: "available" });
  assert.equal(parseNotInstalled(body)?.component, "terminals");
  assert.equal(parseNotInstalled('{"error":"other"}'), null);
  assert.equal(parseNotInstalled("not json"), null);
  assert.equal(parseNotInstalled(""), null);
});

test("the inactive-route store skips matching fetches, never the state reads", () => {
  setInactiveRoutes(["/api/terminal/", "/api/shell-wrap/status", "GET /api/obs/egress/status"]);
  assert.equal(isRouteInactive("/api/terminal/launch?x=1"), true);
  assert.equal(isRouteInactive("/api/shell-wrap/status"), true);
  assert.equal(isRouteInactive("/api/obs/egress/status"), true);
  assert.equal(isRouteInactive("/api/sessions"), false);
  assert.equal(isRouteInactive("/api/components/state"), false);
  setInactiveRoutes([]);
  assert.equal(isRouteInactive("/api/terminal/launch"), false);
});

// The en and em dash, built from char codes so this file carries none.
const DASHES = new RegExp(`[${String.fromCharCode(0x2013)}${String.fromCharCode(0x2014)}]`);

test("copy is honest and dash free", () => {
  for (const s of ["absent", "available", "blocked", "active"] as const) {
    const line = stateCopy(s, "Code intelligence", ["developer", "full"]);
    assert.ok(!DASHES.test(line), line);
  }
  assert.match(stateCopy("absent", "Code intelligence", ["developer"]), /Builds that include it: developer\./);
  assert.match(stateCopy("available", "x"), /nothing runs/);
});

test("distrosWith, groupOf and the manifest", () => {
  assert.deepEqual(distrosWith({ distros: { minimal: "out", developer: "on", full: "off" } }), ["developer", "full"]);
  assert.deepEqual(distrosWith(undefined), []);
  const row = (id: string, cls: CatalogComponent["class"]) => ({ id, class: cls }) as CatalogComponent;
  assert.equal(groupOf(row("adapter.zed", "adapter")), "adapters");
  assert.equal(groupOf(row("integration.hooks", "integration")), "integrations");
  assert.equal(groupOf(row("export.otel", "feature")), "telemetry");
  assert.equal(groupOf(row("org.client", "feature")), "org");
  assert.equal(groupOf(row("demo", "feature")), "helpers");
  assert.equal(groupOf(row("codeintel", "feature")), "features");
  const m = JSON.parse(manifestFor(view));
  assert.deepEqual(m.active, ["core"]);
  assert.equal(m.base, "developer");
});

test("tileVisible (T-670): a tile hides when its data route or its producing component is not active, and fails open", () => {
  setInactiveRoutes([]);
  assert.equal(tileVisible(null, { component: "terminals" }), true, "no view: visible");
  assert.equal(tileVisible(view, { component: "terminals" }), false, "available, not active: hidden");
  assert.equal(tileVisible(view, { component: "core" }), true);
  assert.equal(tileVisible(view, { component: "never-heard-of" }), true, "unknown id: visible");
  assert.equal(tileVisible(view, {}), true);
  // The daemon lists registered patterns: exact routes and "/x/" prefixes.
  setInactiveRoutes(["GET /api/cache/overview", "/api/update/"]);
  assert.equal(tileVisible(view, { route: "/api/cache/overview" }), false, "route of an inactive component: hidden");
  assert.equal(tileVisible(view, { route: "/api/update/status" }), false, "under an inactive prefix: hidden");
  assert.equal(tileVisible(null, { route: "/api/cache/overview" }), false);
  assert.equal(tileVisible(view, { route: "/api/discover" }), true);
  setInactiveRoutes([]);
  assert.equal(tileVisible(view, { route: "/api/cache/overview" }), true);
});

// T-707: the artifact warning rule mirrors cmd/observer artifactWarns, and
// every observed state has a label.
test("artifactWarns matches the doctor rule; every state is labelled", () => {
  const base = { id: "artifact.vscode-extension", detail: "", rule: "r", leftover: false } as const;
  assert.equal(artifactWarns({ ...base, state: "stale" }), true);
  assert.equal(artifactWarns({ ...base, state: "incompatible" }), true);
  assert.equal(artifactWarns({ ...base, state: "host_removed" }), true);
  assert.equal(artifactWarns({ ...base, state: "installed", leftover: true }), true);
  for (const s of ["connected", "installed", "not_detected", "absent", "not_applicable", "policy_blocked"] as const) {
    assert.equal(artifactWarns({ ...base, state: s }), false, s);
  }
  for (const [k, v] of Object.entries(ARTIFACT_STATE_LABEL)) {
    assert.ok(v.length > 0, k);
  }
});
