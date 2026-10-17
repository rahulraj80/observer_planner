import assert from "node:assert/strict";
import test from "node:test";

import {
  PROJECT_DEFAULT_TARGET,
  assignedProjects,
  changeSummary,
  projectAssignments,
  projectName,
  targetLabel,
  targetOptions,
  type ProjectProfilesResponse,
} from "./projectProfiles.ts";

// T-421 item 1: Settings > Profiles "Per-project assignments" renders
// GET /api/config/project-profiles. These pin the derived rows, the target
// picker and the confirm-step summary.

const resp: ProjectProfilesResponse = {
  projects: [
    { root: "/home/dev/alpha", has_file: true, default: "codex-variant", by_tool: { cline: "fast" }, by_provider: { openai: "codex-safe" } },
    { root: "/home/dev/beta", has_file: false },
    { root: "/home/dev/gamma", has_file: true, error: "does not parse" },
  ],
  truncated: false,
  profile_names: ["codex-safe", "codex-variant", "default", "fast"],
  providers: ["anthropic", "openai"],
  tools: [
    { id: "cline", proxied: true },
    { id: "zed", proxied: false },
  ],
  global_default: "default",
};

test("projectAssignments lists the project-wide one first, then providers, then tools", () => {
  assert.deepEqual(projectAssignments(resp.projects[0]), [
    { target: "default", label: "all traffic", profile: "codex-variant" },
    { target: "openai", label: "openai traffic", profile: "codex-safe" },
    { target: "tool:cline", label: "cline", profile: "fast" },
  ]);
  assert.deepEqual(projectAssignments(resp.projects[1]), []);
  assert.deepEqual(projectAssignments(null), []);
});

test("assignedProjects keeps only projects whose file assigns something", () => {
  assert.deepEqual(
    assignedProjects(resp).map((p) => p.root),
    ["/home/dev/alpha"],
  );
  assert.deepEqual(assignedProjects(null), []);
});

test("targetOptions offers the project-wide target, providers, then tools with the proxy caveat", () => {
  assert.deepEqual(targetOptions(resp), [
    { value: PROJECT_DEFAULT_TARGET, label: "All traffic from this project" },
    { value: "anthropic", label: "anthropic traffic" },
    { value: "openai", label: "openai traffic" },
    { value: "tool:cline", label: "cline" },
    { value: "tool:zed", label: "zed (not proxied)" },
  ]);
  assert.deepEqual(targetOptions(null), []);
});

test("changeSummary states the current and the next resolution", () => {
  const alpha = resp.projects[0];
  assert.equal(changeSummary(alpha, "default", "fast"), "alpha, all traffic: codex-variant -> fast");
  assert.equal(changeSummary(alpha, "tool:cline", ""), "alpha, cline: fast -> the global assignment");
  assert.equal(
    changeSummary(resp.projects[1], "anthropic", "codex-safe"),
    "beta, anthropic traffic: the global assignment -> codex-safe",
  );
});

test("projectName and targetLabel", () => {
  assert.equal(projectName("/home/dev/alpha/"), "alpha");
  assert.equal(projectName("C:\\work\\repo"), "repo");
  assert.equal(targetLabel("tool:zed"), "zed");
  assert.equal(targetLabel("default"), "all traffic");
});
