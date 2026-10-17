import assert from "node:assert/strict";
import test from "node:test";

import { setCostDisplayMode } from "../../../shared/lib/format.ts";
import {
  EXPERIMENTAL_WARNING,
  SOURCE_HOWTO,
  SOURCE_LABELS,
  canSave,
  cardPill,
  cardTitle,
  identityText,
  maskIdentity,
  errorText,
  fmtAmount,
  methodOptions,
  windowFrac,
  windowLabel,
  windowValue,
  type CatalogSource,
  type PlanCard,
  type PlanWindow,
} from "./plans.ts";

const base: PlanWindow = {
  key: "5h",
  unit: "percent",
  source: "claude.proxy",
  source_kind: "passive_proxy",
  rung: "official",
  confidence: "exact",
  observed_at: "2026-10-13T10:00:00Z",
};

test("window labels are plain words, never a raw key", () => {
  assert.equal(windowLabel("5h"), "5-hour");
  assert.equal(windowLabel("key_cap"), "Key cap");
  assert.equal(windowLabel("model:opus"), "Weekly, Opus");
  assert.equal(windowLabel("cycle:agent"), "This cycle, Agent");
  assert.doesNotMatch(windowLabel("some_new_window"), /_/);
});

test("a window with no fraction is never drawn as 0%", () => {
  assert.equal(windowFrac({ ...base }), null);
  assert.equal(windowValue({ ...base }), "Not reported");
  assert.equal(windowFrac({ ...base, used_frac: 0.52 }), 0.52);
  assert.equal(windowValue({ ...base, used_frac: 0.52 }), "52%");
  assert.equal(windowValue({ ...base, used_frac: 0.64, confidence: "estimated" }), "~64%");
});

test("money follows the cost display", () => {
  const w: PlanWindow = { ...base, key: "key_cap", unit: "usd_micros", used: 9_100_000, limit: 30_000_000 };
  try {
    setCostDisplayMode("usd");
    assert.match(windowValue(w), /9\.10 of .*30\.00/);
    setCostDisplayMode("tokens");
    assert.equal(windowValue(w), "30%");
    assert.doesNotMatch(fmtAmount("usd_micros", 18_420_000), /\$/);
    assert.doesNotMatch(fmtAmount("cny_micros", 18_420_000), /CN|¥/);
  } finally {
    setCostDisplayMode("usd");
  }
});

test("card titles carry plan and label", () => {
  const c: PlanCard = {
    id: "c",
    provider: "claude",
    sources: [],
    status: "ok",
    health: "fresh",
    plan_code: "max_5x",
    label: "work",
    windows: [],
    balances: [],
    spend: [],
  };
  assert.equal(cardTitle(c), "Claude Max 5x (work)");
  assert.equal(cardTitle({ ...c, plan_code: "", label: "" }), "Claude");
});

test("the add flow never offers a passive source and checks required fields", () => {
  const passive: CatalogSource = { id: "claude.proxy", kind: "passive_proxy", rung: "official", experimental: false, passive: true, secrets: [], params: [] };
  const key: CatalogSource = {
    id: "deepseek.api",
    kind: "official_api",
    rung: "official",
    experimental: false,
    passive: false,
    secrets: [{ key: "key", required: true }],
    params: [],
  };
  assert.deepEqual(methodOptions({ id: "x", group: "plans", has_experimental: false, sources: [passive, key] }), [key]);
  assert.equal(canSave(key, {}, {}), false);
  assert.equal(canSave(key, {}, { key: "sk" }), true);
  const manual: CatalogSource = {
    id: "manual",
    kind: "manual",
    rung: "official",
    experimental: false,
    passive: false,
    secrets: [],
    params: [{ key: "allowance", required: true, numeric: true }],
  };
  assert.equal(canSave(manual, { allowance: "abc" }, {}), false);
  assert.equal(canSave(manual, { allowance: "20" }, {}), true);
});

test("every source has a label and a one-line instruction", () => {
  for (const id of Object.keys(SOURCE_LABELS)) {
    assert.ok(SOURCE_HOWTO[id], `no instruction for ${id}`);
    assert.ok(SOURCE_HOWTO[id].length <= 140, `instruction for ${id} is longer than one line`);
  }
  assert.ok(EXPERIMENTAL_WARNING.length <= 160);
});

test("server errors read as sentences", () => {
  assert.equal(errorText('{"error":"turn on the experimental switch for this provider first"}'), "Turn on the experimental switch for this provider first");
  assert.equal(errorText(""), "The request failed");
});

test("a login never lands in the card title and is masked below it", () => {
  const c: PlanCard = {
    id: "passive:chatgpt:k1",
    provider: "chatgpt",
    sources: [],
    status: "ok",
    health: "fresh",
    plan_label: "Pro",
    identity: "santosh@example.com",
    windows: [],
    balances: [],
    spend: [],
  };
  assert.equal(cardTitle(c), "ChatGPT and Codex Pro");
  assert.equal(identityText(c), "Signed in as s***h@example.com");
  // An email that reached the label (an older server) stays out of the title too.
  const old = { ...c, identity: undefined, label: "santosh@example.com" };
  assert.equal(cardTitle(old), "ChatGPT and Codex Pro");
  assert.equal(identityText(old), "Signed in as s***h@example.com");
  assert.equal(identityText({ ...c, identity: "" }), "");
  assert.equal(maskIdentity("ab@x.io"), "a***@x.io");
  assert.equal(maskIdentity("octocat"), "o***t");
});

test("a card pill never claims a refresh is running", () => {
  assert.equal(cardPill({ status: "ok", health: "fresh" }), null);
  assert.equal(cardPill({ status: "ok", health: "stale" })?.label, "Out of date");
  assert.equal(cardPill({ status: "throttled", health: "stale" })?.label, "Limit reached");
  for (const health of ["fresh", "stale", "waiting"]) {
    assert.notEqual(cardPill({ status: "ok", health })?.label, "Updating");
  }
});
