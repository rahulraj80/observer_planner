import assert from "node:assert/strict";
import test from "node:test";

import type { WatcherHealth } from "./types.ts";
import { WATCHER_LINE_LABEL, watcherLineState } from "./watcherLine.ts";

const base: WatcherHealth = {
  files: [],
  behind_count: 0,
  behind_total_bytes: 0,
  orphan_count: 0,
  suspected_misrouted_count: 0,
  checked_at: "2026-10-07T00:00:00Z",
};

test("watcherLineState is active when the daemon runs the watcher (T-585)", () => {
  assert.equal(watcherLineState({ ...base, watcher_running: true }), "active");
});

test("watcherLineState is off for a daemon that runs no watcher", () => {
  assert.equal(watcherLineState({ ...base, watcher_running: false }), "off");
});

test("watcherLineState is unknown before the payload loads or on an older daemon", () => {
  assert.equal(watcherLineState(null), "unknown");
  assert.equal(watcherLineState(undefined), "unknown");
  assert.equal(watcherLineState(base), "unknown");
});

test("each state has a label", () => {
  assert.equal(WATCHER_LINE_LABEL.active, "active");
  assert.equal(WATCHER_LINE_LABEL.off, "off");
  assert.equal(WATCHER_LINE_LABEL.unknown, "-");
});
