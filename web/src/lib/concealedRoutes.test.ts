import { test } from "node:test";
import assert from "node:assert/strict";
import {
  concealedRoutesSnapshot,
  isRouteConcealed,
  routeConcealed,
  routeMatches,
  setConcealedRoutes,
  subscribeConcealedRoutes,
} from "./concealedRoutes.ts";

// T-401: the shared fetch guard matches the daemon's mux patterns.
test("routeMatches follows mux pattern shapes", () => {
  const cases: Array<[string, string, boolean]> = [
    ["/api/cloud/status", "/api/cloud/status", true],
    ["/api/cloud/status?x=1", "/api/cloud/status", true],
    ["/api/cloud/status/extra", "/api/cloud/status", false],
    ["/api/benchmarks/run/7", "/api/benchmarks/", true],
    ["/api/benchmarks", "/api/benchmarks/", false],
    ["/api/project/abc", "/api/project/{id}", true],
    ["/api/project/abc/cost", "/api/project/{id}", false],
    ["/api/project//cost", "/api/project/{id}/cost", false],
    ["/api/project/abc/cost", "/api/project/{id}/cost", true],
    ["/api/files/a/b/c", "/api/files/{rest...}", true],
  ];
  for (const [path, pattern, want] of cases) {
    assert.equal(routeMatches(path, pattern), want, `${path} ~ ${pattern}`);
  }
});

test("routeConcealed never holds the governance read itself", () => {
  assert.equal(routeConcealed("/api/governance", ["/api/"]), false);
  assert.equal(routeConcealed("/api/config/backup", ["/api/config/backup"]), true);
  assert.equal(routeConcealed("/api/config", ["/api/config/backup"]), false);
  assert.equal(routeConcealed("/api/status", []), false);
});

test("the store notifies on a change only and clears on an empty list", () => {
  let calls = 0;
  const off = subscribeConcealedRoutes(() => {
    calls++;
  });
  try {
    setConcealedRoutes(["/api/cloud/status", "/api/config/backup"]);
    assert.equal(calls, 1);
    assert.equal(isRouteConcealed("/api/cloud/status"), true);
    assert.equal(isRouteConcealed("/api/status"), false);
    setConcealedRoutes(["/api/config/backup", "/api/cloud/status"]);
    assert.equal(calls, 1, "same set in another order is not a change");
    setConcealedRoutes(undefined);
    assert.equal(calls, 2);
    assert.equal(isRouteConcealed("/api/cloud/status"), false);
    assert.equal(concealedRoutesSnapshot(), "");
  } finally {
    off();
  }
});
