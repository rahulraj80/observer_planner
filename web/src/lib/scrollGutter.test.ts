import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// T-469: moving between tools in the New Terminal picker made a scrollbar
// gutter appear and disappear, so the layout jittered (the active row expands
// its detail line, which flips the list across its max height). The fix is a
// stable gutter on every bounded vertical scroll region inside a modal,
// drawer, dropdown, picker or palette, owned by ONE constant in
// shared/lib/scroll.ts. This pins the constant and every audited region, so a
// region cannot quietly fall back to a bare `overflow-y-auto`.

const repo = new URL("../../../", import.meta.url);
const read = (path: string) => readFileSync(new URL(path, repo), "utf8");

test("the shared constant reserves a stable vertical gutter", () => {
  const src = read("shared/lib/scroll.ts");
  const m = src.match(/export const STABLE_SCROLL_Y = "([^"]*)"/);
  assert.ok(m, "STABLE_SCROLL_Y not found in shared/lib/scroll.ts");
  const classes = m[1].split(/\s+/);
  assert.ok(classes.includes("overflow-y-auto"));
  assert.ok(classes.includes("[scrollbar-gutter:stable]"));
});

// Every audited scroll region in a modal / drawer / dropdown / picker /
// palette. `marker` is a substring unique to that region's element; the
// region's own class list must carry STABLE_SCROLL_Y and no bare
// overflow-y-auto.
const REGIONS: { file: string; marker: string; what: string }[] = [
  { file: "shared/primitives/Modal.tsx", marker: '"min-h-0 flex-1", STABLE_SCROLL_Y, bodyClassName', what: "Modal body" },
  { file: "shared/primitives/SlideOver.tsx", marker: '"min-h-0 flex-1", STABLE_SCROLL_Y, bodyClassName', what: "SlideOver body" },
  { file: "shared/primitives/ComboChip.tsx", marker: '"max-h-[min(280px,50vh)] py-1", STABLE_SCROLL_Y', what: "ComboChip listbox (the New Terminal tool picker)" },
  { file: "shared/primitives/EntityMultiPicker.tsx", marker: '"max-h-[280px] py-1", STABLE_SCROLL_Y', what: "EntityMultiPicker list" },
  // Both apps' command palettes render the one shared shell (T-465).
  { file: "shared/components/palette/CommandPalette.tsx", marker: '"max-h-[52vh] p-1.5", STABLE_SCROLL_Y', what: "command palette results (node and org)" },
  { file: "web/src/components/TagEditor.tsx", marker: "max-h-[60vh] min-h-0 flex-1 py-1 ${STABLE_SCROLL_Y}", what: "tag editor dropdown" },
  { file: "web2/src/pages/obs/Trajectories.tsx", marker: "mb-2 max-h-48 space-y-0.5 ${STABLE_SCROLL_Y}", what: "Add to dataset dropdown" },
  { file: "web2/src/pages/Posture.tsx", marker: "mb-3 max-h-48 rounded border border-line-2 ${STABLE_SCROLL_Y}", what: "downgrade dialog column list" },
];

for (const r of REGIONS) {
  test(`${r.what} uses the stable scroll gutter`, () => {
    const src = read(r.file);
    assert.ok(src.includes(r.marker), `${r.file}: ${r.what} does not use STABLE_SCROLL_Y`);
    assert.ok(/import \{ STABLE_SCROLL_Y \} from "((\.\.\/)+lib|@shared\/lib)\/scroll";/.test(src), `${r.file} does not import STABLE_SCROLL_Y`);
  });
}

test("the shared primitives keep no bare overflow-y-auto scroll body", () => {
  for (const file of [
    "shared/primitives/Modal.tsx",
    "shared/primitives/SlideOver.tsx",
    "shared/primitives/ComboChip.tsx",
    "shared/primitives/EntityMultiPicker.tsx",
  ]) {
    assert.ok(!read(file).includes("overflow-y-auto"), `${file} still spells overflow-y-auto itself`);
  }
});
