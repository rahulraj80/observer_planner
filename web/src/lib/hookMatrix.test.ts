import { test } from "node:test";
import assert from "node:assert/strict";
import {
  HOOK_COVERAGE_STATE,
  hookCellState,
  hookRowGateWired,
  hookRowHasVendorSurface,
  hookRowObserverWired,
  type HookMatrixRow,
} from "../../../shared/lib/hookMatrix.ts";

function row(states: string[], bindings: HookMatrixRow["bindings"] = []): HookMatrixRow {
  return {
    tool: "t",
    auto_wired: true,
    cells: states.map((s, i) => ({ group: `g${i}`, state: s, mark: "-", events: [] })),
    caps: {},
    bindings,
  };
}

function binding(cap: string, wired: boolean): HookMatrixRow["bindings"][number] {
  return { cap, native_event: "E", decisions: "D", registered: wired, wired, grounding: "doc" };
}

// Every coverage state controlcoverage emits has a presentation row, and an
// unknown state stays honest (neutral, raw value), never a guess.
test("coverage states", () => {
  for (const s of ["wired", "registered", "probe_required", "vendor_only", "none"]) {
    assert.ok(HOOK_COVERAGE_STATE[s], s);
  }
  assert.deepEqual(hookCellState("future_state"), { tone: "neutral", mark: "future_state", label: "future_state" });
  assert.equal(hookCellState("wired").mark, "W");
});

// The row predicates the node filter and the probe button use, one case per
// row of the table.
test("row predicates", () => {
  const cases: Array<{ name: string; r: HookMatrixRow; observer: boolean; vendor: boolean; gate: boolean }> = [
    { name: "nothing", r: row(["none", "none"]), observer: false, vendor: false, gate: false },
    { name: "vendor only", r: row(["vendor_only", "none"]), observer: false, vendor: true, gate: false },
    { name: "probe", r: row(["probe_required"]), observer: true, vendor: true, gate: false },
    {
      name: "registered observe",
      r: row(["registered"], [binding("tool_observe", true)]),
      observer: true,
      vendor: true,
      gate: false,
    },
    {
      name: "wired tool gate",
      r: row(["wired"], [binding("tool_gate", true)]),
      observer: true,
      vendor: true,
      gate: true,
    },
    {
      name: "unwired prompt gate",
      r: row(["vendor_only"], [binding("prompt_gate", false)]),
      observer: false,
      vendor: true,
      gate: false,
    },
  ];
  for (const c of cases) {
    assert.equal(hookRowObserverWired(c.r), c.observer, `${c.name} observer`);
    assert.equal(hookRowHasVendorSurface(c.r), c.vendor, `${c.name} vendor`);
    assert.equal(hookRowGateWired(c.r, ["tool_gate", "prompt_gate"]), c.gate, `${c.name} gate`);
  }
});
