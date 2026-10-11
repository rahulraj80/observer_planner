// chipId: what a picked entity chip shows next to its label.
//
// A chip reads by its label. The id is a secondary hint: hidden when it
// says nothing the label does not (the label IS the id, e.g. a free-typed
// id or a model name), and otherwise kept short and muted so a 32-hex
// member id never crowds the chip out of its row. The full id always
// travels in the tooltip. Pure, so it is unit-tested.

/** Ids longer than this are shortened on the chip. */
export const CHIP_ID_MAX = 12;

/** How many leading characters a shortened id keeps. */
export const CHIP_ID_KEEP = 8;

/** chipIdHint returns the id text to render beside a chip's label, or null
 *  when the id should not render at all (no label, or label equals id). */
export function chipIdHint(id: string, label: string | undefined): string | null {
  if (!label || label.trim() === id.trim()) return null;
  if (id.length <= CHIP_ID_MAX) return id;
  return `${id.slice(0, CHIP_ID_KEEP)}…`;
}
