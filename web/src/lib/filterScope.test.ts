import assert from "node:assert/strict";
import test from "node:test";
import { barNoteFilters, filterScopeFor, ignoredFilters, ignoredFiltersNote } from "./filterScope.ts";

const cases: Array<[string, string]> = [
  ["/", "window,tool,project,model"],
  ["", "window,tool,project,model"],
  ["/cost", "window,tool,project,model"],
  ["/sessions", "window,tool,project,model"],
  ["/sessions/extra", "window,tool,project,model"],
  ["/patterns", "tool,project,model"],
  ["/projects", "window,tool,project,model"],
  ["/routing", "window,model"],
  ["/live", "model"],
  ["/benchmarks", "model"],
  ["/search", ""],
  ["/settings", ""],
  ["/terminals", ""],
  ["/remote", ""],
  ["/no-such-page", ""],
];
for (const [path, want] of cases) {
  test(`filterScopeFor(${JSON.stringify(path)}) = [${want}]`, () => {
    const s = filterScopeFor(path);
    const got = (["window", "tool", "project", "model"] as const).filter((k) => s[k]).join(",");
    assert.equal(got, want);
  });
}

test("ignoredFilters names only set filters the page does not read", () => {
  assert.deepEqual(ignoredFilters(filterScopeFor("/settings"), { tool: "codex", project: "" }), ["tool"]);
  assert.deepEqual(ignoredFilters(filterScopeFor("/routing"), { tool: "codex", project: "p1" }), ["tool", "project"]);
  assert.deepEqual(ignoredFilters(filterScopeFor("/cost"), { tool: "codex", project: "p1" }), []);
  assert.deepEqual(ignoredFilters(filterScopeFor("/settings"), { tool: "", project: "" }), []);
  // "all" is the FilterProvider's unfiltered default, not a set filter.
  assert.deepEqual(ignoredFilters(filterScopeFor("/settings"), { tool: "all", project: "all" }), []);
  // The model filter follows the same rule.
  assert.deepEqual(ignoredFilters(filterScopeFor("/search"), { tool: "all", project: "all", model: "gpt-5.1" }), ["model"]);
  assert.deepEqual(ignoredFilters(filterScopeFor("/routing"), { tool: "all", project: "all", model: "gpt-5.1" }), []);
  assert.deepEqual(ignoredFilters(filterScopeFor("/cost"), { tool: "all", project: "all", model: "gpt-5.1" }), []);
  assert.deepEqual(ignoredFilters(filterScopeFor("/settings"), { tool: "all", project: "all", model: "all" }), []);
});

test("ignoredFiltersNote reads naturally for one, two and three filters", () => {
  assert.equal(ignoredFiltersNote([]), "");
  assert.equal(ignoredFiltersNote(["model"]), "The model filter does not apply to this page");
  assert.equal(ignoredFiltersNote(["tool", "project"]), "Tool and project filters do not apply to this page");
  assert.equal(
    ignoredFiltersNote(["tool", "project", "model"]),
    "Tool, project and model filters do not apply to this page",
  );
});

test("a tab row overrides its page: the Cost statement reads only the model", () => {
  const s = filterScopeFor("/cost", "?tab=statement&month=2026-10");
  assert.deepEqual(s, { window: false, tool: false, project: false, model: true });
  // Other Cost tabs keep the page row.
  assert.deepEqual(filterScopeFor("/cost", "?tab=overview"), filterScopeFor("/cost"));
  // A tab value with no row falls through to the page.
  assert.deepEqual(filterScopeFor("/sessions", "?tab=statement"), filterScopeFor("/sessions"));
  assert.deepEqual(
    ignoredFilters(s, { tool: "codex", project: "/p", model: "gpt-5.1" }),
    ["tool", "project"],
  );
});

test("barNoteFilters always names the model filter where the page does not read it", () => {
  // Unset: still named, because the always-present Model chip is disabled here.
  assert.deepEqual(barNoteFilters(filterScopeFor("/settings"), { tool: "all", project: "all", model: "all" }), ["model"]);
  // Set and ignored: named once.
  assert.deepEqual(barNoteFilters(filterScopeFor("/settings"), { tool: "codex", project: "all", model: "gpt-5.1" }), ["tool", "model"]);
  // Read by the page: not named.
  assert.deepEqual(barNoteFilters(filterScopeFor("/cost"), { tool: "all", project: "all", model: "all" }), []);
  assert.deepEqual(barNoteFilters(filterScopeFor("/live"), { tool: "all", project: "all", model: "gpt-5.1" }), []);
});
