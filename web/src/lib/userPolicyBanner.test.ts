import assert from "node:assert/strict";
import test from "node:test";

import { userPolicyNotice } from "./userPolicyBanner.ts";

test("userPolicyNotice renders nothing when the file loads (no problem on the summary)", () => {
  assert.equal(userPolicyNotice(undefined, true), null);
  assert.equal(userPolicyNotice(null, true), null);
  assert.equal(userPolicyNotice({ path: "", error: "x" }, true), null);
});

test("userPolicyNotice names the file, the line and the error", () => {
  const n = userPolicyNotice(
    { path: "/home/u/.observer/guard-policy.toml", line: 3, error: "parse: toml: line 3: expected value" },
    true,
  );
  assert.equal(n?.location, "/home/u/.observer/guard-policy.toml, line 3");
  assert.match(n?.error ?? "", /expected value/);
  assert.match(n?.consequence ?? "", /Built-in rules and the org policy stay in force/);
});

test("userPolicyNotice omits an unknown line and says so when the guard is off", () => {
  const n = userPolicyNotice({ path: "/p/guard-policy.toml", error: "read: permission denied" }, false);
  assert.equal(n?.location, "/p/guard-policy.toml");
  assert.match(n?.consequence ?? "", /The guard is off today/);
});

test("userPolicyNotice copy has no em dash", () => {
  const n = userPolicyNotice({ path: "/p", line: 1, error: "e" }, true);
  assert.ok(n);
  for (const s of [n.title, n.consequence]) assert.ok(!s.includes("—"), s);
});

test("userPolicyNotice says the org blocks guarded calls under fail_closed (D-221)", () => {
  const n = userPolicyNotice({ path: "/p/guard-policy.toml", line: 2, error: "e", fail_closed: true }, true);
  assert.match(n?.title ?? "", /organization blocks guarded calls/);
  assert.match(n?.consequence ?? "", /blocked until the file loads/);
  for (const s of [n?.title ?? "", n?.consequence ?? ""]) assert.ok(!s.includes("\u2014"), s);
  // With the guard off nothing is blocked, so the fail-closed copy never shows.
  const off = userPolicyNotice({ path: "/p", error: "e", fail_closed: true }, false);
  assert.match(off?.consequence ?? "", /The guard is off today/);
});
