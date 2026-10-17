import assert from "node:assert/strict";
import test from "node:test";

import {
  HONESTY_TONE,
  selectionKey,
  sideLabel,
  stateLabel,
  statusParams,
  targetApiId,
  targetOptions,
  unavailableRows,
} from "./shellWrap.ts";
import type { ShellWrapStatus, ShellWrapTarget, ShellWrapTool } from "./types.ts";

const base: ShellWrapStatus = {
  goos: "linux",
  enabled: false,
  shim_dir: "/h/.observer/shims",
  observer_path: "/usr/bin/observer",
  shells: ["bash", "zsh", "fish"],
  detected_shells: null,
  planned_shells: null,
  active: false,
  in_sync: true,
  tools: null,
  rc: null,
  orphans: null,
  warnings: null,
};

test("selectionKey ignores order", () => {
  assert.equal(selectionKey(["b", "a"], ["zsh", "bash"]), selectionKey(["a", "b"], ["bash", "zsh"]));
  assert.notEqual(selectionKey(["a"], []), selectionKey(["a", "b"], []));
  assert.notEqual(selectionKey(["a"], ["bash"]), selectionKey(["a"], []));
});

test("stateLabel table", () => {
  const rows: [Partial<ShellWrapStatus>, string][] = [
    [{ enabled: true, in_sync: true }, "on"],
    [{ enabled: true, in_sync: false }, "on - files differ, re-apply"],
    [{ enabled: false, active: true }, "off - leftovers installed"],
    [{}, "off"],
  ];
  for (const [patch, want] of rows) {
    assert.equal(stateLabel({ ...base, ...patch }).text, want);
  }
});

test("every honesty value has a tone", () => {
  assert.deepEqual(Object.keys(HONESTY_TONE).sort(), ["conditional", "launch_only", "proof_owed", "routed"]);
});

const native: ShellWrapTarget = { id: "", label: "WSL (Ubuntu)", goos: "linux", available: true, enabled: false };
const windows: ShellWrapTarget = {
  id: "windows",
  label: "Windows (via WSL Ubuntu)",
  goos: "windows",
  available: true,
  enabled: false,
};

test("targetOptions shows the switch only with a second target", () => {
  assert.deepEqual(targetOptions(null), []);
  assert.deepEqual(targetOptions([native]), []);
  const opts = targetOptions([native, windows]);
  assert.deepEqual(
    opts.map((o) => [o.value, o.disabled]),
    [
      ["native", false],
      ["windows", false],
    ],
  );
  const off = targetOptions([native, { ...windows, available: false, reason: "observer is not running inside WSL" }]);
  assert.equal(off[1].disabled, true);
  assert.equal(off[1].disabledReason, "observer is not running inside WSL");
});

test("target ids and query params", () => {
  assert.equal(targetApiId("native"), "");
  assert.equal(targetApiId("windows"), "windows");
  assert.equal(statusParams("native"), undefined);
  assert.deepEqual(statusParams("windows"), { target: "windows" });
});

test("sideLabel tags bridge rows only", () => {
  const tool = { side: "wsl" } as ShellWrapTool;
  assert.equal(sideLabel(base, tool), null);
  const bridged = { ...base, goos: "windows", bridge: { distro: "Ubuntu" } };
  assert.equal(sideLabel(bridged, tool), "runs in WSL");
  assert.equal(sideLabel(bridged, { side: "host" } as ShellWrapTool), "Windows program");
});

test("unavailable rows: every server row is listed with its reason, command line first, then by label", () => {
  assert.deepEqual(unavailableRows(null), []);
  assert.deepEqual(unavailableRows({ unavailable: null }), []);
  const rows = unavailableRows({
    unavailable: [
      { id: "zed", label: "Zed", kind: "gui", reason: "no grounded launch command" },
      { id: "qoder", label: "qoder", kind: "terminal", reason: "no command name on this OS", hint: "observer qoder" },
      { id: "aider", label: "aider", kind: "terminal", reason: "not installed" },
    ],
  });
  assert.deepEqual(rows.map((r) => r.id), ["aider", "qoder", "zed"]);
  assert.equal(rows[1].reason, "no command name on this OS");
  assert.equal(rows[1].hint, "observer qoder");
});
