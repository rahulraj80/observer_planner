import assert from "node:assert/strict";
import test from "node:test";

import {
  STATUS_LABELS,
  buildToolPickerItems,
  preferredDefaultTool,
  type PickerGUIRow,
  type ToolPickerInput,
} from "./toolPicker.ts";

const NAMES: Record<string, string> = {
  "claude-code": "Claude Code",
  codex: "Codex",
  "antigravity-cli": "Antigravity CLI",
  junie: "Junie",
};
const labelFor = (id: string) => NAMES[id] ?? id;

function gui(over: Partial<PickerGUIRow> & { id: string }): PickerGUIRow {
  return {
    label: over.id,
    adapter: "x",
    surface: "ide",
    allowed: false,
    watched: true,
    hosts: [],
    ...over,
  };
}

function input(over: Partial<ToolPickerInput> = {}): ToolPickerInput {
  return {
    tools: ["antigravity-cli", "claude-code", "codex"],
    toolInfo: [
      { tool: "antigravity-cli", allowed: false, watched: true },
      { tool: "claude-code", allowed: true, watched: true },
      { tool: "codex", allowed: true, watched: false },
    ],
    gui: [],
    shellEnabled: true,
    shellTool: "shell",
    labelFor,
    ...over,
  };
}

test("groups follow the table order: CLI, IDE, Desktop, Shell", () => {
  const items = buildToolPickerItems(
    input({
      gui: [
        gui({ id: "opencode-desktop", surface: "desktop" }),
        gui({ id: "idea", surface: "ide" }),
      ],
    }),
  );
  assert.deepEqual(
    items.map((i) => i.group),
    ["CLI agents", "CLI agents", "CLI agents", "IDEs", "Desktop apps", "Shell"],
  );
  assert.deepEqual(
    items.map((i) => i.kindLabel),
    ["CLI", "CLI", "CLI", "IDE", "Desktop", "Shell"],
  );
});

test("launchable rows sort first within a group, otherwise server order", () => {
  const items = buildToolPickerItems(input());
  assert.deepEqual(
    items.filter((i) => i.kind === "cli").map((i) => i.value),
    ["claude-code", "codex", "antigravity-cli"],
  );
});

// One case per status row: the status comes from capability flags only.
const statusCases: Array<{ name: string; info?: { allowed: boolean; watched: boolean }; want: string | null }> = [
  { name: "unknown (old daemon) is never 'not allowed'", info: undefined, want: null },
  { name: "refused by the launch allow-list", info: { allowed: false, watched: true }, want: "not_allowed" },
  { name: "refusal leads even when also unwatched", info: { allowed: false, watched: false }, want: "not_allowed" },
  { name: "allowed but not captured", info: { allowed: true, watched: false }, want: "not_captured" },
  { name: "allowed and captured", info: { allowed: true, watched: true }, want: null },
];
for (const c of statusCases) {
  test(`terminal tool status: ${c.name}`, () => {
    const items = buildToolPickerItems(
      input({ tools: ["codex"], toolInfo: c.info ? [{ tool: "codex", ...c.info }] : [] }),
    );
    const row = items.find((i) => i.value === "codex")!;
    assert.equal(row.status, c.want);
    assert.equal(row.statusLabel, c.want ? STATUS_LABELS[c.want as keyof typeof STATUS_LABELS] : null);
  });
}

test("every AI-tool and GUI row stays selectable, even when not allowed (install is not gated)", () => {
  const items = buildToolPickerItems(
    input({ gui: [gui({ id: "vscode", adapter: "", allowed: false })] }),
  );
  for (const i of items.filter((x) => x.kind !== "shell")) {
    assert.equal(i.disabled, false, i.value);
  }
});

test("plain-words status label replaces 'not allow-listed'", () => {
  const items = buildToolPickerItems(input());
  const refused = items.find((i) => i.value === "antigravity-cli")!;
  assert.equal(refused.statusLabel, "Not allowed to launch");
  assert.ok(!items.some((i) => `${i.name} ${i.statusLabel ?? ""} ${i.detail ?? ""}`.includes("allow-listed")));
});

test("host rows carry the agent list as a detail line; adapter rows do not", () => {
  const items = buildToolPickerItems(
    input({
      gui: [
        gui({ id: "idea", label: "IntelliJ IDEA", adapter: "", hosts: ["junie", "claude-code", "codex"] }),
        gui({ id: "antigravity-ide", adapter: "antigravity", hosts: ["antigravity"] }),
      ],
    }),
  );
  assert.equal(items.find((i) => i.value === "idea")!.detail, "Agents: Junie, Claude Code, Codex");
  assert.equal(items.find((i) => i.value === "antigravity-ide")!.detail, null);
});

test("a GUI row's colour dot comes from its adapter; a host row's from its own id", () => {
  const items = buildToolPickerItems(
    input({
      gui: [
        gui({ id: "claude-desktop", adapter: "cowork", surface: "desktop" }),
        gui({ id: "vscode", adapter: "" }),
      ],
    }),
  );
  assert.equal(items.find((i) => i.value === "claude-desktop")!.dotKey, "cowork");
  assert.equal(items.find((i) => i.value === "vscode")!.dotKey, "vscode");
  assert.equal(items.find((i) => i.value === "codex")!.dotKey, "codex");
});

test("a host row is never flagged 'not captured' (it has no capture of its own)", () => {
  const items = buildToolPickerItems(
    input({ gui: [gui({ id: "vscode", adapter: "", allowed: true, watched: false })] }),
  );
  assert.equal(items.find((i) => i.value === "vscode")!.status, null);
});

test("an adapter-backed GUI row that is allowed but unwatched is 'not captured'", () => {
  const items = buildToolPickerItems(
    input({ gui: [gui({ id: "opencode-desktop", surface: "desktop", allowed: true, watched: false })] }),
  );
  assert.equal(items.find((i) => i.value === "opencode-desktop")!.status, "not_captured");
});

test("shell: disabled with a reason when allow_shell is off, selectable when on", () => {
  const off = buildToolPickerItems(input({ shellEnabled: false })).find((i) => i.kind === "shell")!;
  assert.equal(off.disabled, true);
  assert.equal(off.status, "shell_off");
  assert.match(off.detail ?? "", /allow_shell/);
  const on = buildToolPickerItems(input({ shellEnabled: true })).find((i) => i.kind === "shell")!;
  assert.equal(on.disabled, false);
  assert.equal(on.status, null);
  assert.equal(on.value, "shell");
});

test("search matches the display name and the id, not the host list", () => {
  const items = buildToolPickerItems(
    input({ gui: [gui({ id: "idea", label: "IntelliJ IDEA", adapter: "", hosts: ["claude-code"] })] }),
  );
  const hit = (q: string) => items.filter((i) => i.searchable.includes(q)).map((i) => i.value);
  assert.deepEqual(hit("claude"), ["claude-code"]);
  assert.deepEqual(hit("intellij"), ["idea"]);
  assert.deepEqual(hit("antigravity-cli"), ["antigravity-cli"]);
});

const defaultCases: Array<{ name: string; tools: string[]; info: Array<{ tool: string; allowed: boolean; watched: boolean }>; want: string }> = [
  { name: "first allowed tool wins over list order", tools: ["a", "b"], info: [{ tool: "a", allowed: false, watched: true }, { tool: "b", allowed: true, watched: true }], want: "b" },
  { name: "nothing allowed falls back to the first tool", tools: ["a", "b"], info: [{ tool: "a", allowed: false, watched: true }, { tool: "b", allowed: false, watched: true }], want: "a" },
  { name: "no annotations (old daemon) keeps the first tool", tools: ["a", "b"], info: [], want: "a" },
  { name: "empty list", tools: [], info: [], want: "" },
];
for (const c of defaultCases) {
  test(`preferredDefaultTool: ${c.name}`, () => {
    assert.equal(preferredDefaultTool(c.tools, c.info), c.want);
  });
}

test("advertised products the dashboard cannot start are listed last, disabled, with the reason (T-494)", () => {
  const items = buildToolPickerItems(
    input({
      unlaunchable: [
        { tool: "chatgpt-web", kind: "browser", note: "Used in your web browser." },
        { tool: "cline", kind: "hosted", host: "Visual Studio Code", note: "Runs inside Visual Studio Code." },
      ],
    }),
  );
  const tail = items.slice(-2);
  assert.deepEqual(
    tail.map((i) => [i.value, i.group, i.disabled, i.detail, i.dotKey]),
    [
      ["unlaunchable:chatgpt-web", "Not launchable here", true, "Used in your web browser.", "chatgpt-web"],
      ["unlaunchable:cline", "Not launchable here", true, "Runs inside Visual Studio Code.", "cline"],
    ],
  );
  // Never a launch id: no selectable row shares a value with them.
  assert.equal(items.filter((i) => !i.disabled && i.value.startsWith("unlaunchable:")).length, 0);
  // An older daemon without the field lists none.
  assert.equal(buildToolPickerItems(input()).filter((i) => i.kind === "unlaunchable").length, 0);
});
