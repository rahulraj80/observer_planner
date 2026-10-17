import assert from "node:assert/strict";
import test from "node:test";

import {
  PROVIDER_FALLBACK,
  assignmentRows,
  assignmentTarget,
  changePreview,
  isNoop,
  pickerTools,
  toolOptionLabel,
  type ProfileAssignmentsResponse,
} from "./profileAssign.ts";

// T-369: Settings > Profiles "Per-tool assignments" renders
// GET /api/config/profile-assignments. These pin the derived rows, the
// picker and the confirm-step preview.

const resp: ProfileAssignmentsResponse = {
  default: "default",
  by_provider: { anthropic: "claude-code", openai: "codex-safe" },
  by_tool: { cline: "codex-variant", "legacy-tool": "codex-safe", zed: "codex-safe" },
  profile_names: ["claude-code", "codex-safe", "codex-variant", "default"],
  tools: [
    { id: "zed", proxied: false },
    { id: "cline", proxied: true },
    { id: "codex", proxied: true },
    { id: "aider", proxied: true },
    { id: "kiro-crew", proxied: false },
  ],
  changed: false,
};

test("assignmentRows lists every assigned tool, sorted, with its honesty flags", () => {
  assert.deepEqual(assignmentRows(resp), [
    { tool: "cline", profile: "codex-variant", unknown: false, notProxied: false },
    { tool: "legacy-tool", profile: "codex-safe", unknown: true, notProxied: false },
    { tool: "zed", profile: "codex-safe", unknown: false, notProxied: true },
  ]);
  assert.deepEqual(assignmentRows(null), []);
});

test("pickerTools offers only unassigned tools, proxied ones first", () => {
  assert.deepEqual(
    pickerTools(resp).map((t) => t.id),
    ["aider", "codex", "kiro-crew"],
  );
  assert.equal(toolOptionLabel({ id: "kiro-crew", proxied: false }), "kiro-crew (not proxied)");
  assert.equal(toolOptionLabel({ id: "codex", proxied: true }), "codex");
});

test("changePreview states the current and the next resolution", () => {
  assert.equal(changePreview(resp, "cline", "codex-safe"), "cline: codex-variant -> codex-safe");
  assert.equal(changePreview(resp, "aider", "codex-safe"), `aider: ${PROVIDER_FALLBACK} -> codex-safe`);
  assert.equal(changePreview(resp, "cline", ""), `cline: codex-variant -> ${PROVIDER_FALLBACK}`);
});

test("isNoop and the target spelling match the daemon's", () => {
  assert.equal(isNoop(resp, "cline", "codex-variant"), true);
  assert.equal(isNoop(resp, "cline", "codex-safe"), false);
  assert.equal(isNoop(resp, "aider", ""), true);
  assert.equal(assignmentTarget("cline"), "tool:cline");
});
