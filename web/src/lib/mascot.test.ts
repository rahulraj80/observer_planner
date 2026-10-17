// Pins shared/lib/mascot.ts (the artwork behind shared/primitives/Obs.tsx):
// the named parts per state, the small-size cape switch at the threshold,
// the reduced-motion gate, and that every colour is a --mascot-* token
// defined in BOTH theme blocks of shared/styles/tokens.css.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  MASCOT_TOKENS,
  SMALL_CAPE_MAX_PX,
  capePath,
  mascotCss,
  mascotNodes,
  type MascotNode,
  type MascotPart,
  type MascotState,
} from "../../../shared/lib/mascot.ts";

function walk(nodes: MascotNode[], out: MascotNode[] = []): MascotNode[] {
  for (const n of nodes) {
    out.push(n);
    if (n.children) walk(n.children, out);
  }
  return out;
}
const partsOf = (nodes: MascotNode[]) =>
  new Set(walk(nodes).map((n) => n.attrs["data-part"]).filter((p): p is string => typeof p === "string"));
const find = (nodes: MascotNode[], p: MascotPart) => walk(nodes).filter((n) => n.attrs["data-part"] === p);

const ALWAYS: MascotPart[] = ["ground", "hop", "tilt", "cape", "arms", "feet", "body", "screen", "face", "badge", "blush"];
const CASES: Array<{ state: MascotState; has: MascotPart[]; lacks: MascotPart[] }> = [
  { state: "idle", has: [...ALWAYS, "cursor", "cape-lining"], lacks: ["burst", "sparkle", "thinking"] },
  { state: "celebrate", has: [...ALWAYS, "burst", "sparkle", "cape-lining"], lacks: ["cursor", "thinking"] },
  { state: "lost", has: [...ALWAYS, "thinking", "cursor", "cape-lining"], lacks: ["burst", "sparkle"] },
];

for (const c of CASES) {
  test(`mascot ${c.state}: named parts`, () => {
    const parts = partsOf(mascotNodes(c.state, 96));
    for (const p of c.has) assert.ok(parts.has(p), `${c.state} is missing part ${p}`);
    for (const p of c.lacks) assert.ok(!parts.has(p), `${c.state} must not draw ${p}`);
  });
}

test("cape switches to the simplified silhouette at the threshold", () => {
  for (const state of ["idle", "celebrate", "lost"] as MascotState[]) {
    const small = mascotNodes(state, SMALL_CAPE_MAX_PX);
    const big = mascotNodes(state, SMALL_CAPE_MAX_PX + 1);
    assert.equal(find(small, "cape-lining").length, 0, `${state} at ${SMALL_CAPE_MAX_PX}px has no lining`);
    assert.equal(find(big, "cape-lining").length, 1, `${state} above the threshold has a lining`);
    assert.notEqual(capePath(state, SMALL_CAPE_MAX_PX), capePath(state, SMALL_CAPE_MAX_PX + 1));
    const capeFill = (nodes: MascotNode[]) => find(nodes, "cape")[0].children!.at(-1)!.attrs.d;
    assert.equal(capeFill(small), capePath(state, 24));
    assert.equal(capeFill(big), capePath(state, 160));
  }
  // The small sparkle is dropped too; the burst stays.
  assert.equal(find(mascotNodes("celebrate", 32), "sparkle").length, 0);
  assert.equal(find(mascotNodes("celebrate", 32), "burst").length, 1);
});

test("every animation sits inside the reduced-motion gate", () => {
  for (const state of ["idle", "celebrate", "lost"] as MascotState[]) {
    const css = mascotCss("sb-mascot-x", state);
    const gate = "@media (prefers-reduced-motion: no-preference){";
    const at = css.indexOf(gate);
    assert.ok(at > 0, `${state}: has the no-preference block`);
    const before = css.slice(0, at);
    assert.doesNotMatch(before, /animation|@keyframes/, `${state}: no motion outside the gate`);
    assert.match(css.slice(at), /animation:/, `${state}: animates inside the gate`);
    assert.ok(css.endsWith("}}"), `${state}: the gate closes at the end`);
    assert.match(css, /\.sb-mascot-x \[data-part="/, `${state}: selectors are scoped`);
  }
  assert.match(mascotCss("s", "idle"), /data-part="cursor"\]\{animation/);
  assert.match(mascotCss("s", "idle"), /data-part="cape"\]\{animation/);
  assert.match(mascotCss("s", "celebrate"), /data-part="burst"\]\{animation/);
  assert.match(mascotCss("s", "celebrate"), /data-part="hop"\]\{animation/);
  assert.match(mascotCss("s", "lost"), /data-part="thinking"\] circle\{animation/);
});

test("colours are tokens only, and lost uses the lost tokens", () => {
  const ALLOWED_LITERALS = new Set(["#ffffff", "none"]); // the gloss highlight; unfilled strokes
  for (const state of ["idle", "celebrate", "lost"] as MascotState[]) {
    const used = new Set<string>();
    for (const n of walk(mascotNodes(state, 96))) {
      for (const key of ["fill", "stroke"]) {
        const val = n.attrs[key];
        if (typeof val !== "string") continue;
        if (ALLOWED_LITERALS.has(val)) continue;
        const m = /^var\((--[a-z0-9-]+)\)$/.exec(val);
        assert.ok(m, `${state}: ${n.tag}.${key}=${val} is not a token`);
        used.add(m[1]);
      }
    }
    for (const t of used) assert.ok(t === "--fg-4" || (MASCOT_TOKENS as readonly string[]).includes(t), `${state}: unknown token ${t}`);
    const lostOnly = [...used].filter((t) => t.startsWith("--mascot-lost-"));
    if (state === "lost") {
      assert.ok(used.has("--mascot-lost-body") && used.has("--mascot-lost-cape"), "lost greys the body and cape");
      assert.ok(!used.has("--mascot-body") && !used.has("--mascot-cape"));
    } else {
      assert.equal(lostOnly.length, 0, `${state} must not use lost tokens`);
    }
  }
});

test("every --mascot-* token is defined in both theme blocks", () => {
  const css = readFileSync(new URL("../../../shared/styles/tokens.css", import.meta.url), "utf8");
  const lightAt = css.indexOf(':root[data-theme="light"]');
  assert.ok(lightAt > 0);
  const dark = css.slice(0, lightAt);
  const light = css.slice(lightAt, css.indexOf("/* Density", lightAt));
  for (const t of MASCOT_TOKENS) {
    assert.match(dark, new RegExp(`${t}:\\s*#[0-9a-f]{6};`), `dark theme defines ${t}`);
    assert.match(light, new RegExp(`${t}:\\s*#[0-9a-f]{6};`), `light theme defines ${t}`);
  }
});
