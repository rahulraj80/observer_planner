// Pins the daemon-restart question's copy (restartConfirm.ts) for each probe
// outcome: what a restart does, the live proxy count, and, only when a
// session is routing through the daemon, how to avoid a dropped request and
// why the route-off script runs from a shell.
import { test } from "node:test";
import assert from "node:assert/strict";
import { restartConfirmBody } from "./restartConfirm.ts";

test("a routed session gets the count, the between-turns advice and why the script is a shell step", () => {
  const body = restartConfirmBody("This applies your pending config changes.", { available: true, proxy_recent_requests: 3 });
  assert.equal(body[0], "This applies your pending config changes.");
  assert.match(body[1], /shuts down gracefully and starts again in place/);
  assert.match(body[2], /3 requests in the last minute/);
  assert.match(body[2], /drops its in-flight proxy requests/);
  assert.match(body[3], /restart between turns/);
  assert.match(body[3], /does not take the proxy route off first/);
  assert.match(body[3], /scripts\/restart-daemon\.sh does \(route off, restart, route on\)/);
  // The script is never named without the reason it is a shell step.
  assert.match(body[3], /runs from a shell because it stops and relaunches the daemon from outside it/);
});

test("one request is singular", () => {
  const body = restartConfirmBody("lead", { available: true, proxy_recent_requests: 1 });
  assert.match(body[2], /\(1 request in the last minute\)/);
});

test("no traffic says no session should notice and names no script", () => {
  const body = restartConfirmBody("lead", { available: true, proxy_recent_requests: 0 });
  assert.equal(body.length, 3);
  assert.match(body[2], /no coding session should notice/);
  assert.ok(!body.join(" ").includes("restart-daemon.sh"));
});

test("an unavailable probe falls back to the generic warning", () => {
  const body = restartConfirmBody("lead", null);
  assert.equal(body.length, 3);
  assert.match(body[2], /may drop one in-flight request/);
});
