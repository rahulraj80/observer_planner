// Pure helpers of the Terminals > Sandboxes tab (lib/sandboxesTab.ts).

import { test } from "node:test";
import assert from "node:assert/strict";
import {
  adviseBody,
  adviseReady,
  durationText,
  findingsText,
  forkTabCrumb,
  networkForEgress,
  nodeError,
  statusText,
  tierForClass,
  tierTitle,
  withForkTabs,
  type SandboxRun,
} from "./sandboxesTab.ts";
import { TERMINALS_TABS } from "./nodePageTabs.ts";

test("the Sandboxes tab sits after Workspace and stays out of the governed table", () => {
  assert.deepEqual(withForkTabs(TERMINALS_TABS.ids), ["workspace", "sandboxes", "settings", "arena"]);
  assert.ok(!(TERMINALS_TABS.ids as readonly string[]).includes("sandboxes"), "a governed tab would bump the org vocabulary");
  assert.equal(forkTabCrumb("/terminals", "?tab=sandboxes"), "Sandboxes");
  assert.equal(forkTabCrumb("/terminals", "?tab=arena"), null);
  assert.equal(forkTabCrumb("/security", "?tab=sandboxes"), null);
});

test("isolation and network read in SandHold's words", () => {
  assert.deepEqual(
    ["process", "container", "userspace_kernel", "hw_vm", "none", ""].map(tierForClass),
    ["Shared", "Shielded", "Sealed", "Vault", "", ""],
  );
  assert.deepEqual(["none", "proxy_only", "internet"].map(networkForEgress), ["None", "Limited", "Open"]);
  assert.equal(networkForEgress(undefined), "");
  assert.equal(tierTitle("vault"), "Vault");
});

const run = (over: Partial<SandboxRun>): SandboxRun => ({
  run_id: "r1",
  tool: "claude-code",
  kind: "fresh",
  launched_at: "2026-11-11T10:00:00Z",
  state: "ended",
  backend: "bwrap",
  isolation_class: "process",
  ...over,
});

test("findings show only what was recorded", () => {
  assert.equal(findingsText(run({})), "");
  assert.equal(findingsText(run({ refused: 1 })), "1 connection refused");
  assert.equal(findingsText(run({ refused: 3, refused_may_be_more: true })), "3 connections refused, maybe more");
  assert.equal(findingsText(run({ refused_may_be_more: true })), "connections may have been refused");
});

test("status and duration", () => {
  assert.equal(statusText(run({ exit_code: 0 })), "ended, exit 0");
  assert.equal(statusText(run({ state: "running" })), "running");
  assert.equal(statusText(run({ state: "abandoned" })), "lost (no live terminal)");
  assert.deepEqual([48, 840, 7500, undefined].map(durationText), ["48 s", "14 min", "2 h 5 min", ""]);
});

test("the advisor's answers", () => {
  assert.equal(adviseReady({ runs: "agent", trust: "", reach: [] }), false);
  assert.equal(adviseReady({ runs: "agent", trust: "generated", reach: [] }), true);
  assert.deepEqual(adviseBody({ runs: "tests", trust: "generated", reach: ["pypi"] }, " GitHub.com, github.com  api.example.com"), {
    runs: "tests",
    trust: "generated",
    reach: { packages: ["pypi"], hosts: ["github.com", "api.example.com"] },
  });
  assert.deepEqual(nodeError('{"error":"trust \\"maybe\\" is not one of the answers","fix":"say own"}'), {
    error: 'trust "maybe" is not one of the answers',
    fix: "say own",
  });
  assert.deepEqual(nodeError("plain text\n"), { error: "plain text" });
});
