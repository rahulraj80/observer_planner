// Pins the Requests page's grant-cache line (review M4-09, M4-14).
import { test } from "node:test";
import assert from "node:assert/strict";
import { grantsCacheLine, sentToLine } from "./grantsCache.ts";

const dur = (s: number) => `${s}s`;
const base = { cached: true, lastVerifiedNs: 1_000_000_000, maxOfflineS: 86400, orgName: "Acme", nowNs: 61_000_000_000 };

test("a verified cache says confirmed, with the org's name", () => {
  assert.equal(grantsCacheLine(base, dur), "Confirmed with Acme 60s ago. Offline, an exception holds for at most 86400s after the last confirmation.");
});

test("an unverified cache never says confirmed (M4-09)", () => {
  const line = grantsCacheLine({ ...base, unverified: true }, dur);
  assert.ok(!line.includes("Confirmed"), line);
  assert.match(line, /no longer verify/);
});

test("nothing cached", () => {
  assert.equal(grantsCacheLine({ ...base, cached: false }, dur), "Nothing confirmed with the organization yet.");
});

test("an org id is never shown as the org's name (M4-14, T-160)", () => {
  const id = "0123456789abcdef0123456789abcdef";
  assert.ok(!grantsCacheLine({ ...base, orgName: id }, dur).includes(id));
  assert.equal(sentToLine(id), "Sent to your organization. Admins answer from their Requests queue.");
  assert.equal(sentToLine("Acme", "unreachable"), "unreachable");
});
