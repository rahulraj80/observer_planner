import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";

// The shared Button's disabled look must be visibly different from every
// enabled variant, in particular `secondary`, which shares its fill and
// border. Before 2026-10-03 the two differed only by fg-2 vs fg-3 text, so
// the managed Cloud Intelligence page's disabled "Sign in" / "Sync now"
// looked clickable. The primitive is a .tsx (React), so this reads the
// class constants from source rather than importing the component.
const src = readFileSync(new URL("../../../shared/primitives/Button.tsx", import.meta.url), "utf8");

function constString(name: string): string {
  const m = src.match(new RegExp(`const ${name} =\\s*"([^"]*)"`));
  assert.ok(m, `${name} constant not found in shared/primitives/Button.tsx`);
  return m[1];
}

function variantString(name: string): string {
  const m = src.match(new RegExp(`\\b${name}: "([^"]*)"`));
  assert.ok(m, `${name} variant not found in shared/primitives/Button.tsx`);
  return m[1];
}

test("disabled Button is dimmed, uses a not-allowed cursor and a muted label", () => {
  const disabled = constString("DISABLED").split(/\s+/);
  assert.ok(disabled.includes("cursor-not-allowed"));
  assert.ok(disabled.some((c) => /^opacity-\d+$/.test(c)), "disabled must reduce opacity");
  assert.ok(disabled.includes("text-fg-4"));
  // Never a faded accent fill: the disabled fill stays the opaque bg-bg-3.
  assert.ok(disabled.includes("bg-bg-3"));
});

test("disabled Button does not read like an enabled secondary Button", () => {
  const disabled = new Set(constString("DISABLED").split(/\s+/));
  const secondary = variantString("secondary").split(/\s+/).filter((c) => !c.startsWith("hover:"));
  const extra = [...disabled].filter((c) => !secondary.includes(c) && c !== "cursor-not-allowed");
  assert.ok(extra.length >= 2, `disabled adds too little over secondary: ${extra.join(" ")}`);
});

// T-339: a hand-rolled button whose class string cannot know its disabled
// state appends buttonDisabledLook, which must be exactly DISABLED as
// `disabled:` variants (literal, so Tailwind generates every class).
test("buttonDisabledLook is DISABLED as disabled: variants", () => {
  const m = src.match(/export const buttonDisabledLook =\s*"([^"]*)"/);
  assert.ok(m, "buttonDisabledLook not found in shared/primitives/Button.tsx");
  const want = constString("DISABLED").split(/\s+/).map((c) => `disabled:${c}`);
  assert.deepEqual(m[1].split(/\s+/).sort(), want.sort());
});

// T-339: a solid accent control must not be disabled by opacity alone (a
// faded accent fill still reads as clickable in dark mode). Every web class
// string with a solid `bg-accent` and a `disabled:` style takes the disabled
// fill too (the Button component or buttonDisabledLook).
test("no solid-accent button is disabled by opacity alone", () => {
  const root = new URL("../", import.meta.url);
  const offenders: string[] = [];
  const walk = (dir: URL) => {
    for (const ent of readdirSync(dir, { withFileTypes: true })) {
      const u = new URL(ent.name + (ent.isDirectory() ? "/" : ""), dir);
      if (ent.isDirectory()) walk(u);
      else if (/\.tsx?$/.test(ent.name) && !/\.test\.tsx?$/.test(ent.name)) {
        const text = readFileSync(u, "utf8");
        for (const m of text.matchAll(/["`]([^"`]*)["`]/g)) {
          const cls = m[1].split(/\s+/);
          if (cls.includes("bg-accent") && cls.some((c) => c.startsWith("disabled:opacity")) && !cls.includes("disabled:bg-bg-3")) {
            offenders.push(`${u.pathname.split("/src/")[1]}: ${m[1].slice(0, 80)}`);
          }
        }
      }
    }
  };
  walk(root);
  assert.deepEqual(offenders, []);
});
