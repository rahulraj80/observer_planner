import assert from "node:assert/strict";
import test from "node:test";

import {
  ARENA_PREFS_KEY,
  CUSTOM_MODEL,
  buildArenaModelItems,
  loadArenaPrefs,
  modelForSubmit,
  modelLabel,
  parseArenaPrefs,
  saveArenaPrefs,
  shortDate,
  type ArenaHarnessModels,
} from "./arenaModels.ts";

const claude: ArenaHarnessModels = {
  tool: "claude-code",
  selectable: true,
  catalog: "vendor",
  providers: ["anthropic"],
  installed: true,
  options: [
    { model: "claude-opus-4-8", group: "used", source: "tool_history", count: 1234, last_used: "2026-09-30T10:00:00Z", tools: ["claude-code"] },
    { model: "opus", group: "available", source: "alias" },
    { model: "claude-sonnet-5-5", group: "available", source: "catalog" },
  ],
};

const opencode: ArenaHarnessModels = {
  tool: "opencode",
  selectable: true,
  catalog: "open",
  default_model: "openrouter/stealth/ox-alpha",
  note: "routes through OpenRouter",
  installed: false,
  options: [
    { model: "openrouter/stealth/ox-alpha", group: "used", source: "tool_history", count: 1, used_as: "stealth/ox-alpha", tools: ["opencode"] },
    { model: "gpt-5.5", group: "unroutable", source: "node_history", disabled: true, reason: "no provider path" },
  ],
};

test("Default is always first and names a routed default", () => {
  const a = buildArenaModelItems(claude, "claude-code", "");
  assert.equal(a[0].value, "");
  assert.equal(a[0].name, "Default");
  assert.match(a[0].detail, /claude-code's own default/);
  const b = buildArenaModelItems(opencode, "opencode", "");
  assert.match(b[0].detail, /openrouter\/stealth\/ox-alpha/);
});

test("groups follow the server order with worded details", () => {
  const items = buildArenaModelItems(claude, "claude-code", "");
  assert.deepEqual(
    items.map((i) => [i.value, i.group ?? null]),
    [
      ["", null],
      ["claude-opus-4-8", "Used on this machine"],
      ["opus", "Available for claude-code"],
      ["claude-sonnet-5-5", "Available for claude-code"],
      [CUSTOM_MODEL, "Custom"],
    ],
  );
  assert.equal(items[1].detail, "1,234 turns · last used 2026-09-30 · by claude-code");
  assert.match(items[2].detail, /Alias claude-code resolves/);
  assert.equal(items[3].detail, "Anthropic model in the price catalog");
});

test("unroutable rows stay visible, disabled, with the reason", () => {
  const items = buildArenaModelItems(opencode, "opencode", "");
  const row = items.find((i) => i.value === "gpt-5.5");
  assert.ok(row);
  assert.equal(row.disabled, true);
  assert.equal(row.detail, "no provider path");
  assert.equal(row.group, "Not routable in Arena");
  const used = items.find((i) => i.value === "openrouter/stealth/ox-alpha");
  assert.match(used!.detail, /recorded as stealth\/ox-alpha/);
});

test("a typed or remembered id outside the list gets its own row", () => {
  const items = buildArenaModelItems(claude, "claude-code", "claude-haiku-4-5");
  const row = items.find((i) => i.value === "claude-haiku-4-5");
  assert.equal(row?.group, "Custom");
  assert.equal(items[items.length - 1].value, CUSTOM_MODEL);
  // A value already in the list is not duplicated.
  const again = buildArenaModelItems(claude, "claude-code", "opus");
  assert.equal(again.filter((i) => i.value === "opus").length, 1);
});

test("a non-selectable or unloaded harness offers Default only", () => {
  const locked: ArenaHarnessModels = { ...claude, selectable: false, reason: "no flag", options: claude.options };
  assert.deepEqual(buildArenaModelItems(locked, "x", "opus").map((i) => i.value), [""]);
  assert.deepEqual(buildArenaModelItems(undefined, "x", "").map((i) => i.value), [""]);
});

test("modelForSubmit never sends a sentinel, a disabled row or a locked harness's model", () => {
  assert.equal(modelForSubmit(claude, " opus "), "opus");
  assert.equal(modelForSubmit(claude, ""), "");
  assert.equal(modelForSubmit(claude, CUSTOM_MODEL), "");
  assert.equal(modelForSubmit(opencode, "gpt-5.5"), "");
  assert.equal(modelForSubmit({ ...claude, selectable: false }, "opus"), "");
  assert.equal(modelForSubmit(undefined, "typed-id"), "typed-id");
});

test("modelLabel and shortDate", () => {
  assert.equal(modelLabel(""), "harness default");
  assert.equal(modelLabel("opus"), "opus");
  assert.equal(shortDate("2026-09-30T10:00:00.123Z"), "2026-09-30");
  assert.equal(shortDate(undefined), "");
});

test("prefs round-trip and tolerate garbage and failing storage", () => {
  const mem = new Map<string, string>();
  const store = { getItem: (k: string) => mem.get(k) ?? null, setItem: (k: string, v: string) => void mem.set(k, v) };
  saveArenaPrefs(store, { judgeTool: "codex", tools: ["claude-code", "aider"], models: { aider: "gpt-5.5" }, judgeModels: { codex: "o3" } });
  assert.ok(mem.has(ARENA_PREFS_KEY));
  assert.deepEqual(loadArenaPrefs(store), {
    judgeTool: "codex",
    tools: ["claude-code", "aider"],
    models: { aider: "gpt-5.5" },
    judgeModels: { codex: "o3" },
  });
  assert.deepEqual(parseArenaPrefs("{not json"), { models: {}, judgeModels: {} });
  assert.deepEqual(parseArenaPrefs('{"models":{"a":1,"b":"__custom__","c":"x"},"tools":[2,""]}'), {
    models: { c: "x" },
    judgeModels: {},
  });
  const broken = {
    getItem: () => {
      throw new Error("blocked");
    },
    setItem: () => {
      throw new Error("blocked");
    },
  };
  assert.deepEqual(loadArenaPrefs(broken), { models: {}, judgeModels: {} });
  assert.doesNotThrow(() => saveArenaPrefs(broken, { models: {}, judgeModels: {} }));
  assert.deepEqual(loadArenaPrefs(undefined), { models: {}, judgeModels: {} });
});
