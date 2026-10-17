import assert from "node:assert/strict";
import test from "node:test";

import { installPrereqRemedyFor } from "./installPrereq.ts";
import {
  installRuntimeAdvisory,
  installRuntimeParts,
  installRuntimeRemedy,
  launchRuntimeAdvisory,
  launchRuntimeParts,
  launchRuntimeRemedy,
  runtimeAdvisory,
  runtimeRemedyParts,
} from "./runtimeCheck.ts";
import type { PreflightRuntime, ToolPreflight } from "./types.ts";

// The node-1 shape (2026-10-04): Gemini CLI needs Node.js 20, the host has 18.
const command = "curl -fsSL https://deb.nodesource.com/setup_lts.x | sudo -E bash - && sudo apt-get install -y nodejs";
const note = "Node.js LTS from NodeSource (Debian/Ubuntu). The distribution's own nodejs package is too old on many releases.";
const summary = "Gemini CLI needs Node.js 20 or newer. This machine has 18.19.1.";

const tooOld: PreflightRuntime = {
  runtime: "node",
  display: "Node.js",
  product: "Gemini CLI",
  min: "20",
  have: "18.19.1",
  path: "/usr/bin/node",
  status: "too_old",
  remedy: `Gemini CLI needs Node.js 20 or newer. This machine has 18.19.1 (/usr/bin/node).\nInstall a newer Node.js first:\n  ${command}\n${note}`,
  summary,
  fix_command: command,
  fix_note: note,
  floor_from: "registry",
};

const base = { tool: "gemini-cli", verdict: "ok", can_install: false } as ToolPreflight;

test("too_old splits into summary, path, command and note", () => {
  assert.deepEqual(runtimeRemedyParts(tooOld), { summary, path: "/usr/bin/node", display: "Node.js", command, note });
});

const nonBlocking: Array<{ name: string; rt: PreflightRuntime | undefined }> = [
  { name: "absent (older daemon or nothing applies)", rt: undefined },
  { name: "ok", rt: { ...tooOld, status: "ok", summary: undefined, have: "22.23.3" } },
  { name: "unknown never blocks", rt: { ...tooOld, status: "unknown" } },
  { name: "missing is said elsewhere", rt: { ...tooOld, status: "missing" } },
];
for (const c of nonBlocking) {
  test(`no remedy: ${c.name}`, () => {
    assert.equal(runtimeRemedyParts(c.rt), null);
    assert.equal(launchRuntimeRemedy({ ...base, launch_runtime: c.rt }), null);
    assert.equal(installRuntimeRemedy({ ...base, install_runtime: c.rt }), null);
  });
}

test("launch and install read their own field", () => {
  assert.equal(launchRuntimeRemedy({ ...base, launch_runtime: tooOld }), summary);
  assert.equal(installRuntimeParts({ ...base, launch_runtime: tooOld }), null);
  assert.equal(installRuntimeRemedy({ ...base, verdict: "not_found", install_runtime: tooOld }), summary);
  assert.equal(launchRuntimeParts({ ...base, verdict: "not_found", install_runtime: tooOld }), null);
  assert.equal(launchRuntimeParts(null), null);
});

test("no grounded command: the note is the advice, no command block", () => {
  const p = runtimeRemedyParts({ ...tooOld, fix_command: undefined, fix_note: "Download the LTS release from https://nodejs.org/en/download." });
  assert.equal(p?.command, "");
  assert.equal(p?.note, "Download the LTS release from https://nodejs.org/en/download.");
});

test("an older server with only the plain remedy keeps it as the note", () => {
  const p = runtimeRemedyParts({ ...tooOld, summary: undefined, fix_command: undefined, fix_note: undefined });
  assert.equal(p?.summary, summary);
  assert.equal(p?.command, "");
  assert.equal(p?.note, tooOld.remedy);
});

test("an older server with nothing but the status still says what to do", () => {
  const p = runtimeRemedyParts({ runtime: "node", display: "Node.js", min: "20", status: "too_old" });
  assert.equal(p?.summary, "This tool needs Node.js 20 or newer.");
  assert.equal(p?.note, "Install a newer Node.js first, then press Re-check.");
});

test("the prerequisite box no longer carries the runtime remedy", () => {
  const pf = { ...base, verdict: "not_found", install_runtime: tooOld } as ToolPreflight;
  assert.equal(installPrereqRemedyFor(pf), null);
});

test("no em dash in any fallback copy", () => {
  const p = runtimeRemedyParts({ runtime: "node", display: "Node.js", min: "20", status: "too_old" });
  assert.ok(!JSON.stringify(p).includes("—"));
});

// T-396: outside_range is said, never blocking.
const outside: PreflightRuntime = {
  runtime: "node",
  display: "Node.js",
  product: "Gemini CLI",
  min: "20",
  have: "24.1.0",
  path: "/usr/bin/node",
  status: "outside_range",
  range: ">=20 <23",
  summary: "Gemini CLI declares Node.js >=20 <23. This machine has 24.1.0, which that range leaves out, so the tool may fail to start.",
};

test("outside_range: an advisory sentence, never a remedy", () => {
  assert.equal(runtimeAdvisory(outside), outside.summary);
  assert.equal(runtimeRemedyParts(outside), null);
  assert.equal(launchRuntimeRemedy({ ...base, launch_runtime: outside }), null);
  assert.equal(launchRuntimeAdvisory({ ...base, launch_runtime: outside }), outside.summary);
  assert.equal(installRuntimeAdvisory({ ...base, install_runtime: outside }), outside.summary);
  assert.equal(installRuntimeAdvisory({ ...base, launch_runtime: outside }), null);
});

test("outside_range from an older server without a summary still names the range", () => {
  const got = runtimeAdvisory({ ...outside, summary: undefined });
  assert.equal(got, "Gemini CLI declares Node.js >=20 <23. This machine has 24.1.0, which that range leaves out, so the tool may fail to start.");
  assert.match(runtimeAdvisory({ ...outside, summary: undefined, range: undefined, have: undefined }) ?? "", /a Node\.js version range/);
});

for (const st of ["ok", "too_old", "unknown", "missing"]) {
  test(`no advisory for ${st}`, () => {
    assert.equal(runtimeAdvisory({ ...outside, status: st }), null);
  });
}
