import { test } from "node:test";
import assert from "node:assert/strict";
import { probeLabel } from "./toolProbe.ts";

// T-400: the Connected tools hooks cell reads the server's registry-driven
// probe, including the two states that are not a registration state.
test("probeLabel covers every cell state", () => {
  const cases: Array<[Parameters<typeof probeLabel>[0], string, string]> = [
    [{ registered: false, no_hooks: true, detail: "no hook surface: aider" }, "no hooks", "neutral"],
    [{ registered: false, unprobed: true, detail: "manual" }, "not probed", "neutral"],
    [{ registered: true, detail: "4 events registered" }, "on", "success"],
    [{ registered: false, partial: true }, "partial", "warn"],
    [{ registered: false, detail: "would register 4 events" }, "off", "neutral"],
  ];
  for (const [probe, label, variant] of cases) {
    assert.deepEqual(probeLabel(probe), { label, variant }, JSON.stringify(probe));
  }
});
