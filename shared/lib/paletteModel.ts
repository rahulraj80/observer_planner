// Pure rules of the command palette shell (components/palette/
// CommandPalette.tsx): the flat keyboard order over sectioned rows and the
// cursor moves. No React, no DOM; table-tested.

/** paletteFlatKeys: the row keys in display order (sections top-down). */
export function paletteFlatKeys(sections: readonly { rows: readonly { key: string }[] }[]): string[] {
  return sections.flatMap((s) => s.rows.map((r) => r.key));
}

export type PaletteKey = "ArrowDown" | "ArrowUp" | "Home" | "End";

/**
 * paletteMove: the cursor after a navigation key over `count` rows. The
 * cursor stops at the ends (no wrap, as both apps behaved); an empty list
 * keeps 0.
 */
export function paletteMove(cur: number, key: PaletteKey, count: number): number {
  if (count <= 0) return 0;
  const last = count - 1;
  switch (key) {
    case "ArrowDown":
      return Math.min(last, cur + 1);
    case "ArrowUp":
      return Math.max(0, cur - 1);
    case "Home":
      return 0;
    case "End":
      return last;
  }
}

/** paletteClamp keeps the cursor inside a list that narrowed. */
export function paletteClamp(cur: number, count: number): number {
  return Math.max(0, Math.min(cur, count - 1));
}
