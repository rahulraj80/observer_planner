// Pins the MCP reputation card's model (mcpReputation.ts, T-496).
import { test } from "node:test";
import assert from "node:assert/strict";
import { optInLine, validPackageName } from "./mcpReputation.ts";

test("the opt-in line names exactly the switches still off", () => {
  assert.equal(optInLine({ cloud_enabled: true, reputation_enabled: true }), "");
  assert.match(optInLine({ cloud_enabled: false, reputation_enabled: true }), /\[guard\.cloud\] enabled = true\./);
  const both = optInLine({ cloud_enabled: false, reputation_enabled: false });
  assert.match(both, /\[guard\.cloud\] enabled = true and \[guard\.cloud\.reputation\] enabled = true/);
});

test("package names are checked like the server checks them", () => {
  assert.equal(validPackageName("@modelcontextprotocol/server-filesystem"), true);
  assert.equal(validPackageName("  left-pad "), true);
  assert.equal(validPackageName(""), false);
  assert.equal(validPackageName("a b"), false);
  assert.equal(validPackageName("x".repeat(215)), false);
});
