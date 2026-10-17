// Pins the pure halves of the copy and emphasis primitives (design review
// 2026-10-13, lane DS2; docs/app-design-system.md "Copy rules", "Emphasis",
// "Row actions", "Coverage badge"): RowActions' partition and in-menu
// confirm step, CoverageBadge's status and tone, the copy limits, the one
// loud callout per view budget, and InfoDisclosure's remembered state.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  confirmStep,
  needsConfirm,
  splitRowActions,
  type ConfirmState,
} from "../../../shared/primitives/rowActionsModel.ts";
import {
  COPY_LIMITS,
  COVERAGE_TONE,
  coverageCountLabel,
  coverageFromCounts,
  isLoudCallout,
  loudCalloutsOverBudget,
  overCopyLimit,
  readDisclosureOpen,
  writeDisclosureOpen,
  type DisclosureStorage,
} from "../../../shared/primitives/copyModel.ts";

const A = (id: string, extra: Record<string, unknown> = {}) => ({ id, ...extra });

test("splitRowActions: destructive never inline, safe before destructive", () => {
  const s = splitRowActions([
    A("del", { destructive: true, inline: true }),
    A("edit", { inline: true }),
    A("dup"),
    A("revoke", { destructive: true }),
  ]);
  assert.deepEqual(s.inline.map((a) => a.id), ["edit"]);
  assert.deepEqual(s.menu.map((a) => a.id), ["dup", "del", "revoke"]);
  assert.equal(s.firstDestructive, 1);
});

test("splitRowActions: separator index rows", () => {
  const cases: Array<[Array<ReturnType<typeof A>>, number]> = [
    [[], -1],
    [[A("a")], -1],
    [[A("d", { destructive: true })], 0],
    [[A("e", { inline: true }), A("d", { destructive: true })], 0],
    [[A("a"), A("b"), A("d", { destructive: true })], 2],
  ];
  for (const [actions, want] of cases) {
    assert.equal(splitRowActions(actions).firstDestructive, want, JSON.stringify(actions));
  }
});

test("needsConfirm: destructive confirms unless its own dialog confirms", () => {
  assert.equal(needsConfirm(A("x")), false);
  assert.equal(needsConfirm(A("x", { destructive: true })), true);
  assert.equal(needsConfirm(A("x", { destructive: true, confirm: { label: "Delete?" } })), true);
  assert.equal(needsConfirm(A("x", { destructive: true, confirm: false })), false);
});

test("confirmStep: arm, re-target, fire, disarm, disabled", () => {
  const idle: ConfirmState = { armed: null };
  const del = A("del", { destructive: true });
  const rev = A("rev", { destructive: true });
  const edit = A("edit");

  let r = confirmStep(idle, { type: "choose", action: edit });
  assert.deepEqual(r, { state: { armed: null }, fire: "edit", close: true });

  r = confirmStep(idle, { type: "choose", action: del });
  assert.deepEqual(r, { state: { armed: "del" }, fire: null, close: false });

  const retarget = confirmStep(r.state, { type: "choose", action: rev });
  assert.deepEqual(retarget, { state: { armed: "rev" }, fire: null, close: false });

  const fire = confirmStep(r.state, { type: "choose", action: del });
  assert.deepEqual(fire, { state: { armed: null }, fire: "del", close: true });

  assert.deepEqual(confirmStep(r.state, { type: "disarm" }), { state: { armed: null }, fire: null, close: false });

  const off = confirmStep(r.state, { type: "choose", action: A("del", { destructive: true, disabled: true }) });
  assert.deepEqual(off, { state: r.state, fire: null, close: false });

  const dialog = confirmStep(idle, { type: "choose", action: A("rv", { destructive: true, confirm: false }) });
  assert.deepEqual(dialog, { state: { armed: null }, fire: "rv", close: true });
});

test("coverageFromCounts rows", () => {
  const rows: Array<[number, number, string]> = [
    [0, 0, "unknown"],
    [3, 0, "unknown"],
    [Number.NaN, 3, "unknown"],
    [0, 3, "none"],
    [1, 3, "partial"],
    [3, 3, "full"],
    [4, 3, "full"],
  ];
  for (const [c, t, want] of rows) assert.equal(coverageFromCounts(c, t), want, `${c} of ${t}`);
  assert.equal(coverageCountLabel(1, 3), "1 of 3");
  assert.equal(coverageCountLabel(-2, 3.4), "0 of 3");
});

test("coverage tone: only uncovered rows are loud", () => {
  assert.equal(COVERAGE_TONE.full, "success");
  assert.equal(COVERAGE_TONE.partial, "warn");
  assert.equal(COVERAGE_TONE.none, "danger");
  assert.equal(COVERAGE_TONE.unknown, "neutral");
});

test("copy limits and the one loud callout budget", () => {
  assert.equal(COPY_LIMITS.pageSubtitle, 110);
  assert.equal(overCopyLimit("x".repeat(110), "pageSubtitle"), 0);
  assert.equal(overCopyLimit("  " + "x".repeat(112) + " ", "pageSubtitle"), 2);
  assert.equal(isLoudCallout("info"), false);
  assert.equal(isLoudCallout("success"), false);
  assert.equal(loudCalloutsOverBudget(["info", "warn", "info"]), 0);
  assert.equal(loudCalloutsOverBudget(["warn", "danger", "warn"]), 2);
});

test("disclosure memory: round trip, fallback, blocked storage", () => {
  const m = new Map<string, string>();
  const store: DisclosureStorage = { getItem: (k) => m.get(k) ?? null, setItem: (k, v) => void m.set(k, v) };
  assert.equal(readDisclosureOpen(store, "k", false), false);
  writeDisclosureOpen(store, "k", true);
  assert.equal(readDisclosureOpen(store, "k", false), true);
  writeDisclosureOpen(store, "k", false);
  assert.equal(readDisclosureOpen(store, "k", true), false);
  assert.equal(readDisclosureOpen(null, "k", true), true);
  const blocked: DisclosureStorage = {
    getItem: () => {
      throw new Error("blocked");
    },
    setItem: () => {
      throw new Error("blocked");
    },
  };
  assert.equal(readDisclosureOpen(blocked, "k", true), true);
  assert.doesNotThrow(() => writeDisclosureOpen(blocked, "k", true));
});

// Source pins for the component halves the node test runner cannot render.
const src = (p: string) => readFileSync(new URL(`../../../shared/primitives/${p}`, import.meta.url), "utf8");

test("HelpTooltip is a keyboard-reachable labelled button", () => {
  const s = src("HelpTooltip.tsx");
  assert.match(s, /<button\s+type="button"/);
  assert.match(s, /aria-label=\{/);
  assert.match(s, /data-help-id=\{helpId\}/);
});

test("InfoDisclosure is a real disclosure button", () => {
  const s = src("InfoDisclosure.tsx");
  assert.match(s, /aria-expanded=\{open\}/);
  assert.match(s, /aria-controls=\{panelId\}/);
});

test("RowActions is a labelled ARIA menu that keeps clicks off the row", () => {
  const s = src("RowActions.tsx");
  assert.match(s, /useRole\(context, \{ role: "menu" \}\)/);
  assert.match(s, /role="menuitem"/);
  assert.match(s, /aria-label=\{menuLabel\}/);
  assert.match(s, /onClick=\{stop\} onKeyDown=\{stop\}/);
});

test("SettingRow keeps grid as the default and offers split", () => {
  const s = src("SettingRow.tsx");
  assert.match(s, /layout = "grid"/);
  assert.match(s, /layout\?: "grid" \| "stack" \| "split"/);
});

test("EmptyState renders one centred shape", () => {
  const s = src("EmptyState.tsx");
  assert.doesNotMatch(s, /\bstacked\b/);
  assert.match(s, /flex flex-col items-center gap-3/);
});

test("StatDelta has a neutral tone for metrics with no direction of good", () => {
  const s = src("StatDelta.tsx");
  assert.match(s, /export type StatDeltaTone = "cost" \| "neutral"/);
  assert.match(s, /tone === "neutral" \? "text-fg-3"/);
  assert.match(src("StatCard.tsx"), /tone=\{deltaTone\}/);
  assert.match(src("HeroStat.tsx"), /tone=\{deltaTone\}/);
});
