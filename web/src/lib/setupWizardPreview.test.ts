// Pins the setup wizard's reading of a step reply (setupWizardPreview.ts):
// an error reply is never rendered as a plan, and no line ever says
// "undefined".
import { test } from "node:test";
import assert from "node:assert/strict";
import { summarizeWizardStep as summarizeTelemetry } from "./setupWizardPreview.ts";

test("telemetry step: preview names the edit, a repeat reads as done", () => {
  const plan = summarizeTelemetry("telemetry", { changed: true, summary: ["remove telemetry"], config_path: "/h/.qwen/settings.json" }, false);
  assert.equal(plan.done, false);
  assert.match(plan.line, /will remove telemetry in \/h\/\.qwen\/settings\.json/);
  const again = summarizeTelemetry("telemetry", { changed: false, wired: true, summary: ["already wired"] }, false);
  assert.equal(again.done, true);
});
import { classifyWizardReply, summarizeWizardStep, type WizardStepId } from "./setupWizardPreview.ts";

const six = ["SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest", "PostToolUse", "Stop"];

test("a 403 plain-text reply is an error, never '0 events in undefined'", () => {
  for (const step of ["hooks", "mcp", "route"] as WizardStepId[]) {
    const o = classifyWizardReply(step, 403, "forbidden: cross-origin request\n", false);
    assert.equal(o.kind, "error");
    assert.ok(o.kind === "error" && o.denied);
    assert.match(o.kind === "error" ? o.error : "", /forbidden: cross-origin request/);
  }
});

test("other non-200 replies are errors carrying the server's text or status", () => {
  const o = classifyWizardReply("hooks", 500, '{"error":"boom"}', false);
  assert.deepEqual(o, { kind: "error", denied: false, error: "could not preview this step: boom" });
  const e = classifyWizardReply("mcp", 502, "", true);
  assert.deepEqual(e, { kind: "error", denied: false, error: "could not write this step: HTTP 502" });
});

test("a 409 is a conflict with the server's reason", () => {
  assert.deepEqual(classifyWizardReply("hooks", 409, '{"error":"user hook present"}', false), {
    kind: "conflict",
    error: "user hook present",
  });
});

test("a 200 preview names the real path and the real count", () => {
  const o = classifyWizardReply(
    "hooks",
    200,
    JSON.stringify({ config_path: "/h/.codex/hooks.json", hooks_added: six, already_set: null, dry_run: true }),
    false,
  );
  assert.deepEqual(o, { kind: "plan", done: false, line: "will register 6 events in /h/.codex/hooks.json" });
});

test("no line ever renders 'undefined' when the path is missing", () => {
  const replies = [
    {},
    { hooks_added: six },
    { hooks_added: ["Stop"], already_set: ["SessionStart"] },
    { already_set: ["SessionStart"] },
    { already_set: true },
    { added: true },
    { base_url: "http://127.0.0.1:8820/v1" },
    { added: true, base_url: "http://127.0.0.1:8820/v1" },
  ];
  for (const step of ["hooks", "mcp", "route"] as WizardStepId[]) {
    for (const r of replies) {
      for (const wrote of [false, true]) {
        const { line } = summarizeWizardStep(step, r, wrote);
        assert.ok(!/undefined|null/.test(line), `${step} ${JSON.stringify(r)} wrote=${wrote}: ${line}`);
        assert.ok(line.length > 0);
      }
    }
  }
});

test("hooks lines", () => {
  assert.equal(summarizeWizardStep("hooks", { hooks_added: ["Stop"] }, false).line, "will register 1 event");
  assert.equal(summarizeWizardStep("hooks", {}, false).line, "no hook events to register");
  assert.deepEqual(summarizeWizardStep("hooks", { already_set: six, config_path: "/p" }, false), {
    done: true,
    line: "6 events registered in /p",
  });
  assert.equal(
    summarizeWizardStep("hooks", { hooks_added: ["Stop"], already_set: ["SessionStart"], config_path: "/p" }, false).line,
    "will register 1 event in /p (1 already set)",
  );
});

test("mcp and route lines", () => {
  assert.equal(summarizeWizardStep("mcp", { config_path: "/c.toml" }, false).line, "will add the observer MCP server to /c.toml");
  assert.equal(summarizeWizardStep("mcp", {}, false).line, "will add the observer MCP server");
  assert.equal(summarizeWizardStep("route", {}, false).line, "will set the proxy URL");
  assert.equal(summarizeWizardStep("route", { already_set: true }, false).line, "already routed");
  assert.equal(
    summarizeWizardStep("route", { added: true, base_url: "http://x/v1", config_path: "/c" }, true).line,
    "routed via http://x/v1 (/c)",
  );
});
