import assert from "node:assert/strict";
import test from "node:test";

import {
  badgeText,
  bellLabel,
  moneyText,
  severityTone,
  stateRequestInit,
  unreadIds,
  withoutIds,
  type NotificationRow,
} from "./notificationBell.ts";

const row = (id: string, read: boolean, usd?: number): NotificationRow => ({
  id,
  kind: "cache_expiring",
  severity: "warn",
  title: id,
  at: "2026-10-06T12:00:00Z",
  read,
  usd,
});

test("the badge hides at zero and caps at 9+", () => {
  assert.equal(badgeText(0), "");
  assert.equal(badgeText(-1), "");
  assert.equal(badgeText(3), "3");
  assert.equal(badgeText(12), "9+");
});

test("the label names the unread count", () => {
  assert.equal(bellLabel(0), "Notifications");
  assert.equal(bellLabel(1), "1 unread notification");
  assert.equal(bellLabel(4), "4 unread notifications");
});

test("opening marks only the unread rows read", () => {
  assert.deepEqual(unreadIds([row("a", false), row("b", true), row("c", false)]), ["a", "c"]);
});

test("money shows only where dollars are shown", () => {
  const fmt = (n: number) => `$${n.toFixed(2)}`;
  assert.equal(moneyText(row("a", false, 0.42), true, fmt), "$0.42 at risk");
  // Token-usage display: never a dollar figure from the bell.
  assert.equal(moneyText(row("a", false, 0.42), false, fmt), null);
  assert.equal(moneyText(row("a", false), true, fmt), null);
});

test("severity maps onto the design system's tones", () => {
  assert.equal(severityTone("critical"), "danger");
  assert.equal(severityTone("warn"), "warn");
  assert.equal(severityTone("info"), "accent");
});

test("a dismiss drops the rows optimistically", () => {
  assert.deepEqual(
    withoutIds([row("a", false), row("b", true)], ["a"]).map((r) => r.id),
    ["b"],
  );
});

test("the state write echoes the confirm token as JSON (T-530)", () => {
  const init = stateRequestInit(["a", "b"], "dismiss", "tok");
  assert.equal(init.method, "POST");
  assert.deepEqual(init.headers, { "Content-Type": "application/json", "X-Observer-Confirm": "tok" });
  assert.deepEqual(JSON.parse(String(init.body)), { ids: ["a", "b"], action: "dismiss" });
});
