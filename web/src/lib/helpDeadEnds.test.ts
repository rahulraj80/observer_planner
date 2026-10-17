import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { HELP_REGISTRY } from "./help.ts";

// T-471 (D-003, D-322): copy must not send the user to a CLI for something
// the dashboard already does. Each row is a claim that went stale when a
// dashboard control shipped, with the control that replaced it.
const STALE_CLAIMS: ReadonlyArray<{ claim: RegExp; control: string }> = [
  // T-421 built Connected tools > Set up > "Remove Observer's wiring".
  { claim: /dashboard\s+cannot\s+remove\s+it\s+yet/i, control: "Connected tools" },
];

test("no help entry repeats a stale CLI-only claim", () => {
  for (const e of HELP_REGISTRY) {
    const t = [e.oneLiner, e.detail ?? ""].join("\n");
    for (const { claim } of STALE_CLAIMS) assert.ok(!claim.test(t), `${e.id} says ${claim}`);
  }
});

test("the MCP value-meter help points at the dashboard removal control", () => {
  const e = HELP_REGISTRY.find((x) => x.id === "glossary.settings_mcp");
  assert.ok(e?.detail);
  assert.match(e.detail, /Connected tools/);
  assert.match(e.detail, /Remove Observer's wiring/);
});

test("the Settings MCP card does not repeat a stale CLI-only claim", () => {
  const src = readFileSync(new URL("../pages/Settings.tsx", import.meta.url), "utf8").replace(/\s+/g, " ");
  for (const { claim, control } of STALE_CLAIMS) {
    assert.ok(!claim.test(src), `Settings.tsx says ${claim}`);
    assert.ok(src.includes(control), `Settings.tsx does not name ${control}`);
  }
});
