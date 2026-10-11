import { test } from "node:test";
import assert from "node:assert/strict";
import { HELP_REGISTRY } from "./help.ts";
import { HELP_MANAGED_VIEW, applyPostureView, helpPostureOf } from "./helpPostureView.ts";

const TEXT = ["title", "oneLiner", "detail", "formula", "example"] as const;

function entry(id: string) {
  const e = HELP_REGISTRY.find((x) => x.id === id);
  assert.ok(e, `missing help entry ${id}`);
  return e;
}

function text(e: ReturnType<typeof entry>): string {
  return TEXT.map((k) => e[k] ?? "").join("\n");
}

test("every managed override names a real entry", () => {
  const ids = new Set(HELP_REGISTRY.map((e) => e.id));
  for (const id of Object.keys(HELP_MANAGED_VIEW)) assert.ok(ids.has(id), `stale override ${id}`);
});

test("posture follows the governance managed flag; absent is individual", () => {
  assert.equal(helpPostureOf(true), "managed");
  assert.equal(helpPostureOf(false), "individual");
  assert.equal(helpPostureOf(undefined), "individual");
});

test("an individual machine reads the registry verbatim", () => {
  for (const e of HELP_REGISTRY) assert.equal(applyPostureView(e, "individual"), e);
});

// T-470 / D-007: on an org-managed machine the org ships full content by its
// enrolment grant, so sharing help that promises the developer is the only
// switch is false there.
const MANAGED_FALSE = [
  /no remote switch/i,
  /only if YOU flip/,
  /cannot set, request, or override/i,
  /no org server can flip/i,
  /Deliberate opt-in only/,
];

test("managed: org sharing help says the org ships full content by grant", () => {
  for (const id of ["glossary.settings_org_share", "card.privacy_org_push"]) {
    const t = text(applyPostureView(entry(id), "managed"));
    for (const re of MANAGED_FALSE) assert.ok(!re.test(t), `${id} still says ${re}`);
    assert.match(t, /full content/i, id);
    assert.match(t, /enrolment grant/i, id);
  }
});

test("individual: org sharing help does not claim the server cannot touch it", () => {
  // An organization policy may lower or lock sharing on an individual
  // machine (internal/govern/share.go); only a raise is impossible.
  const t = text(entry("glossary.settings_org_share"));
  assert.ok(!/cannot set, request, or override/i.test(t), t);
  assert.match(t, /only lower or lock/);
});

test("managed overrides carry no dollar figure and no em or en dash", () => {
  for (const [id, o] of Object.entries(HELP_MANAGED_VIEW)) {
    for (const v of Object.values(o)) {
      assert.ok(!/\$\s?\d/.test(v ?? ""), `${id} shows a dollar figure`);
      assert.ok(!/[\u2013\u2014]/.test(v ?? ""), `${id} has an em or en dash`);
    }
  }
});
