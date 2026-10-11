import { test } from "node:test";
import assert from "node:assert/strict";
import { canAskToEdit, configEditChoices } from "./configEditRequest.ts";
import { configEditRequestLink, requestEntryFrom } from "./requestEntry.ts";

test("canAskToEdit: only what the organization stops, only settings the developer edits", () => {
  const user = { id: "codex/model", owner: "user" };
  assert.equal(canAskToEdit({ ...user, locked: "Baseline" }, { read_only: false }), true);
  assert.equal(canAskToEdit(user, { read_only: true }), true);
  assert.equal(canAskToEdit(user, { read_only: false, grants_only: true, grants: ["claude-code/*"] }), true);
  assert.equal(canAskToEdit(user, { read_only: false, grants_only: true, grants: ["codex/*"] }), false);
  assert.equal(canAskToEdit(user, { read_only: false }), false);
  assert.equal(canAskToEdit({ id: "codex/x", owner: "tool_state", locked: "Baseline" }, { read_only: true }), false);
  assert.equal(canAskToEdit(user, null), false);
});

test("configEditChoices: the asked setting first, then each tool's settings", () => {
  assert.deepEqual(configEditChoices("codex/model", ["codex", "claude-code"]), [
    { id: "codex/model", label: "This setting: codex/model" },
    { id: "codex/*", label: "Every codex setting" },
    { id: "claude-code/*", label: "Every claude-code setting" },
  ]);
  assert.deepEqual(configEditChoices(undefined, ["codex"]), [{ id: "codex/*", label: "Every codex setting" }]);
  assert.deepEqual(configEditChoices("codex/*", ["codex"]), [{ id: "codex/*", label: "Every codex setting" }]);
});

test("the edit request link opens an AI tool setting edit request", () => {
  assert.equal(configEditRequestLink("codex/model"), "/requests?new=config&setting=codex%2Fmodel");
  assert.equal(configEditRequestLink(), "/requests?new=config");
  assert.deepEqual(requestEntryFrom(new URLSearchParams("new=config&setting=codex/model")), {
    kind: "type",
    draft: { request_type: "config_edit", message: "", payload: { target: "codex/model" } },
  });
});
