// Pins the setup wizard's Remove panel reading of a removal reply
// (wizardRemove.ts, T-421): an error is never a plan, the preview names the
// exact entries and the file, and an empty removal says so.
import { test } from "node:test";
import assert from "node:assert/strict";
import { classifyRemoveReply } from "./wizardRemove.ts";

test("a hooks preview names every event, the file and what the file keeps", () => {
  const o = classifyRemoveReply(
    "hooks",
    200,
    JSON.stringify({ config_path: "/h/.claude/settings.json", removed: ["Stop", "PreToolUse"], kept: ["Stop"], dry_run: true }),
    false,
  );
  assert.deepEqual(o, {
    kind: "plan",
    nothing: false,
    line: "will remove 2 hook events (Stop, PreToolUse) from /h/.claude/settings.json; keeps your own entries for Stop",
  });
});

test("an MCP removal result and an empty removal", () => {
  assert.deepEqual(classifyRemoveReply("mcp", 200, JSON.stringify({ config_path: "/h/.claude.json", removed: ["observer"] }), true), {
    kind: "plan",
    nothing: false,
    line: "removed the observer MCP server from /h/.claude.json",
  });
  assert.deepEqual(classifyRemoveReply("hooks", 200, JSON.stringify({ removed: [], kept: [] }), false), {
    kind: "plan",
    nothing: true,
    line: "nothing of Observer's to remove",
  });
});

test("a route removal and a foreign route that is kept", () => {
  const o = classifyRemoveReply("route", 200, JSON.stringify({ removed: true, base_url: "http://127.0.0.1:8820", config_path: "/h/.claude/settings.json" }), false);
  assert.equal(o.kind === "plan" && o.line, "will remove the proxy route http://127.0.0.1:8820 from /h/.claude/settings.json");
  const k = classifyRemoveReply("route", 200, JSON.stringify({ removed: false, base_url: "https://gw.example.test" }), false);
  assert.ok(k.kind === "plan" && k.nothing && /kept/.test(k.line));
});

test("409 is a conflict, 403 is a denied error, other failures are errors", () => {
  assert.deepEqual(classifyRemoveReply("hooks", 409, JSON.stringify({ error: "modified since install" }), true), {
    kind: "conflict",
    error: "modified since install",
  });
  const d = classifyRemoveReply("mcp", 403, "forbidden: missing or mismatched confirm token - reload the panel", false);
  assert.ok(d.kind === "error" && d.denied && /confirm token/.test(d.error));
  const e = classifyRemoveReply("hooks", 500, "", true);
  assert.deepEqual(e, { kind: "error", denied: false, error: "could not remove this step: HTTP 500" });
});
