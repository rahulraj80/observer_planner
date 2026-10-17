import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";

import { CLOUD_MANAGED_TITLE, cloudPreviewAvailable, cloudWriteState } from "./cloudGovernance.ts";

// G11: a managed node's read-only `cloud` section must render its write
// controls disabled with the managed explanation, while the read-only
// evidence preview stays available.
test("cloudWriteState: one row per (actionsAvailable, readOnly) combination", () => {
  const cases: Array<{ actions: boolean; ro: boolean; writable: boolean; title?: string }> = [
    { actions: true, ro: false, writable: true },
    { actions: false, ro: false, writable: false },
    { actions: true, ro: true, writable: false, title: CLOUD_MANAGED_TITLE },
    { actions: false, ro: true, writable: false, title: CLOUD_MANAGED_TITLE },
  ];
  for (const c of cases) {
    const got = cloudWriteState(c.actions, c.ro);
    assert.equal(got.writable, c.writable, `writable for actions=${c.actions} ro=${c.ro}`);
    assert.equal(got.lockedTitle, c.title, `title for actions=${c.actions} ro=${c.ro}`);
  }
});

test("cloudPreviewAvailable: read-only never blocks the preview", () => {
  assert.equal(cloudPreviewAvailable(true, true), true);
  assert.equal(cloudPreviewAvailable(true, false), true);
  assert.equal(cloudPreviewAvailable(false, true), false);
});

// M4-13: the cloud section names its own dashboard actions (Sync now, Consent
// grants, the session panel) instead of "run observer cloud ..." where the
// dashboard can already do it. The pointers kept are the honest fallback for a
// dashboard with no action seam wired (not under `observer start`).
test("Cloud section: no CLI pointer where the dashboard already has the action", () => {
  const src = readFileSync(new URL("../pages/settings/CloudIntelligenceSection.tsx", import.meta.url), "utf8");
  const copy = src
    .split("\n")
    .filter((l) => !/^\s*(\/\/|\*|\/\*)/.test(l))
    .join("\n");
  assert.doesNotMatch(copy, /run `observer cloud sync`/);
  assert.doesNotMatch(copy, /run through `observer cloud sync`/);
  assert.doesNotMatch(copy, /CLI:\s*<span className="font-mono">preview/);
  for (const line of copy.split("\n").filter((l) => /observer cloud (login|enable|disable|status)/.test(l))) {
    // Only the no-action-seam fallback may still name these verbs.
    assert.match(line, /otherwise run|no account seam wired|observer cloud login<\/span> in a terminal|observer cloud status<\/span>\)/, line);
  }
});
