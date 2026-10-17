import assert from "node:assert/strict";
import test from "node:test";
import {
  componentOptions,
  contentClassOptions,
  endUserOptions,
  featureOptions,
  guardRuleOptions,
  isModelSetting,
  joinList,
  keepSelected,
  mcpServerOptions,
  modelOptions,
  npmPackagesFromCommands,
  pricedModelIds,
  projectFileOptions,
  projectRootOptions,
  sessionOptions,
  settingValueOptions,
  splitList,
  toolOptions,
  uniqueSorted,
} from "./pickerOptions.ts";

test("uniqueSorted / splitList / joinList", () => {
  assert.deepEqual(uniqueSorted(["b", "", " a ", null, "b", undefined]), ["a", "b"]);
  assert.deepEqual(splitList("json, logs,\ncode,, "), ["json", "logs", "code"]);
  assert.equal(joinList(["json", "logs"]), "json, logs");
});

test("keepSelected keeps a value the list cannot explain visible", () => {
  const opts = [{ id: "a", label: "A" }];
  assert.equal(keepSelected(opts, ["a"]), opts);
  assert.deepEqual(
    keepSelected(opts, ["a", "zz"]).map((o) => [o.id, o.group]),
    [
      ["a", undefined],
      ["zz", "Set in config"],
    ],
  );
});

test("pricedModelIds unions defaults and overrides, sorted", () => {
  assert.deepEqual(pricedModelIds({ "m-b": {}, "m-a": {} }, { "m-c": {}, "m-a": {} }), ["m-a", "m-b", "m-c"]);
  assert.deepEqual(pricedModelIds(null, undefined), []);
});

test("modelOptions: used first by turns, then priced, placeholders dropped", () => {
  const got = modelOptions(
    [
      { key: "claude-haiku-4-5", turn_count: 3 },
      { key: "<unknown>", turn_count: 99 },
      { key: "gpt-5.1", turn_count: 10 },
    ],
    ["claude-haiku-4-5", "claude-opus-4-8", "(unknown)"],
  );
  assert.deepEqual(
    got.map((o) => [o.id, o.group]),
    [
      ["gpt-5.1", "Used on this node"],
      ["claude-haiku-4-5", "Used on this node"],
      ["claude-opus-4-8", "Priced"],
    ],
  );
  assert.equal(got[0].sublabel, "10 turns");
});

test("projectRootOptions: most sessions first, folder name as label", () => {
  const got = projectRootOptions([
    { root_path: "/a/small", session_count: 1 },
    { root_path: "/a/big/", session_count: 9 },
    { root_path: "C:\\work\\win", session_count: 1 },
  ]);
  assert.deepEqual(
    got.map((o) => [o.id, o.label]),
    [
      ["/a/big/", "big"],
      ["/a/small", "small"],
      ["C:\\work\\win", "win"],
    ],
  );
  assert.equal(got[1].sublabel, "1 session");
});

test("sessionOptions: newest first, project folder and tool as label", () => {
  const got = sessionOptions([
    { id: "s1", tool: "codex", project: "/r/alpha", started_at: "2026-10-01T10:00:00Z" },
    { id: "s2", tool: "claude-code", project: "/r/beta", started_at: "2026-10-02T09:30:00Z" },
    { id: "s1", tool: "codex", project: "/r/alpha", started_at: "2026-10-01T10:00:00Z" },
    { id: "s3" },
  ]);
  assert.deepEqual(
    got.map((o) => [o.id, o.label, o.sublabel]),
    [
      ["s2", "beta · claude-code", "2026-10-02 09:30"],
      ["s1", "alpha · codex", "2026-10-01 10:00"],
      ["s3", "s3", undefined],
    ],
  );
});

test("toolOptions: detected tools first, every known tool listed", () => {
  const got = toolOptions([
    { tool: "zed", detected: false },
    { tool: "codex", detected: true },
    { tool: "aider", detected: false },
  ]);
  assert.deepEqual(
    got.map((o) => [o.id, o.group]),
    [
      ["codex", "Detected on this machine"],
      ["aider", "Not detected"],
      ["zed", "Not detected"],
    ],
  );
});

test("guardRuleOptions groups by category", () => {
  const got = guardRuleOptions([{ id: "R-110", category: "git", severity: "high", doc: "force push" }]);
  assert.deepEqual(got, [{ id: "R-110", label: "R-110 force push", sublabel: "high", group: "git" }]);
});

test("mcpServerOptions: one row per name, clients as sublabel", () => {
  const got = mcpServerOptions([
    { name: "fs", client: "cursor" },
    { name: "fs", client: "claude-code" },
    { name: "git" },
    { name: "" },
  ]);
  assert.deepEqual(
    got.map((o) => [o.id, o.sublabel]),
    [
      ["fs", "claude-code, cursor"],
      ["git", undefined],
    ],
  );
});

test("contentClassOptions: criterion, severity, and the composite", () => {
  const got = contentClassOptions(["pii"], ["", "high"]);
  assert.deepEqual(
    got.map((o) => o.id),
    ["pii", "high", "pii/high"],
  );
});

test("endUserOptions: verdict users, then names only the draft carries", () => {
  const got = endUserOptions([{ user: "u2" }, { user: "u1" }, {}, { user: "u2" }], ["u1", "u9", ""]);
  assert.deepEqual(
    got.map((o) => [o.id, o.group]),
    [
      ["u1", "Seen on verdicts"],
      ["u2", "Seen on verdicts"],
      ["u9", "Named in this policy"],
    ],
  );
});

test("featureOptions: node features, then pinned settings", () => {
  const got = featureOptions(["routing.mode", "terminals"]);
  assert.deepEqual(
    got.map((o) => [o.id, o.group]),
    [
      ["terminals", "Node features"],
      ["remote", "Node features"],
      ["routing_apply", "Node features"],
      ["patterns_write", "Node features"],
      ["routing.mode", "Settings your organization pins"],
    ],
  );
});

test("componentOptions: core is never offered, absent builds are disabled", () => {
  const got = componentOptions([
    { id: "core.store", class: "core", title: "Store" },
    { id: "adapter.zed", class: "adapter", title: "Zed", compiled: false },
    { id: "codeintel", class: "feature", title: "Code intelligence", compiled: true },
  ]);
  assert.deepEqual(
    got.map((o) => [o.id, o.group, o.disabledReason]),
    [
      ["adapter.zed", "adapter", "Not in this build"],
      ["codeintel", "feature", undefined],
    ],
  );
});

test("npmPackagesFromCommands: npx / bunx / dlx, versions dropped", () => {
  const cases: [string | undefined, string[]][] = [
    ["npx -y @modelcontextprotocol/server-filesystem /home/me", ["@modelcontextprotocol/server-filesystem"]],
    ["npx @scope/pkg@1.2.3", ["@scope/pkg"]],
    ["/usr/bin/npx --yes plain-pkg@latest", ["plain-pkg"]],
    ["npx.cmd -y win-pkg", ["win-pkg"]],
    ["bunx some-server", ["some-server"]],
    ["pnpm dlx dlx-pkg", ["dlx-pkg"]],
    ["npx --package named-pkg run-it", ["named-pkg"]],
    ["uvx mcp-server-fetch", []],
    ["node ./server.js", []],
    [undefined, []],
  ];
  for (const [cmd, want] of cases) assert.deepEqual(npmPackagesFromCommands([cmd]), want, String(cmd));
});

test("projectFileOptions: project-relative, file actions only, most touched first", () => {
  const got = projectFileOptions(
    [
      { action_type: "read_file", target: "/r/p/src/a.go" },
      { action_type: "edit_file", target: "/r/p/src/a.go" },
      { action_type: "write_file", target: "/r/p/README.md" },
      { action_type: "run_command", target: "/r/p/x.sh" },
      { action_type: "read_file", target: "/elsewhere/b.go" },
    ],
    "/r/p/",
  );
  assert.deepEqual(
    got.map((o) => [o.id, o.sublabel]),
    [
      ["src/a.go", "touched 2 times"],
      ["README.md", "touched 1 time"],
    ],
  );
});

test("isModelSetting / settingValueOptions", () => {
  for (const p of ["model", "agent.model", "small_model", "modelName", "model_id"]) assert.ok(isModelSetting(p), p);
  for (const p of ["theme", "modeline", "remodel_x"]) assert.ok(!isModelSetting(p), p);
  const got = settingValueOptions(["a", "", 3, "a", "b"], [{ id: "b", label: "b" }, { id: "m", label: "m", group: "Priced" }]);
  assert.deepEqual(
    got.map((o) => [o.id, o.group]),
    [
      ["a", "Already used"],
      ["b", "Already used"],
      ["m", "Priced"],
    ],
  );
});
