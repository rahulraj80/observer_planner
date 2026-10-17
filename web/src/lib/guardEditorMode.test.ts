// T-762 (D-580): Security > Rules > "+ Add override" opened with the Rule
// picker disabled, so no rule could be chosen. RuleManager passed
// `initial={editor.initial ?? emptyGuardOverride()}` and the override editor
// locked the picker on `!!initial`, which was therefore always true. The
// opener now names its mode and the table below decides the lock.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { overrideEditorView } from "./guardEditorMode.ts";

const src = (rel: string) => readFileSync(new URL(rel, import.meta.url), "utf8");

test("each opener mode decides the rule lock, title and button", () => {
  const rows = [
    { mode: "add", rule: "", locked: false, title: "Override a built-in rule", save: "Add override", isNew: true },
    { mode: "preset", rule: "R-101", locked: true, title: "Override R-101", save: "Add override", isNew: true },
    { mode: "edit", rule: "R-101", locked: true, title: "Edit override - R-101", save: "Save override", isNew: false },
  ] as const;
  for (const r of rows) {
    const v = overrideEditorView(r.mode, r.rule);
    assert.equal(v.ruleLocked, r.locked, `${r.mode}: ruleLocked`);
    assert.equal(v.title, r.title, `${r.mode}: title`);
    assert.equal(v.saveLabel, r.save, `${r.mode}: saveLabel`);
    assert.equal(v.isNew, r.isNew, `${r.mode}: isNew`);
  }
});

test("the override editor locks the Rule picker from the mode, never from the presence of an initial row", () => {
  const editor = src("../pages/security/RuleEditor.tsx");
  const start = editor.indexOf("function OverrideEditor(");
  const end = editor.indexOf("function CustomRuleEditor(");
  assert.ok(start >= 0 && end > start, "OverrideEditor and CustomRuleEditor are both found");
  const body = editor.slice(start, end);
  assert.doesNotMatch(body, /disabled=\{!!initial\}/, "the Rule picker must not lock on !!initial");
  assert.match(body, /disabled=\{view\.ruleLocked\}/, "the Rule picker locks on the mode's view");
});

test("every override opener in RuleManager names its mode", () => {
  const mgr = src("../pages/security/RuleManager.tsx");
  const openers = mgr.match(/setEditor\(\{ kind: "override"[^}]*/g) ?? [];
  assert.equal(openers.length, 3, "add, edit and the effective-rules Override path");
  assert.ok(openers.some((o) => o.includes('mode: "add"') && !o.includes("initial")), '"+ Add override" opens in add mode with no row');
  assert.ok(openers.some((o) => o.includes('mode: "edit"') && o.includes("initial: r")), "an existing override opens in edit mode");
  assert.ok(openers.some((o) => o.includes('mode: "preset"') && o.includes("rule: id")), "the per-rule Override opens preset to its rule");
  assert.match(mgr, /mode=\{editor\.mode\}/, "the editor receives the opener's mode");
});
