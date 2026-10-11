// overlayEscape: the ONE Escape rule shared by every overlay and every
// control that can sit inside one.
//
//   - An inner popover (EntityMultiPicker's list, ComboChip, ConfirmButton's
//     reason field, DateRangePopover) that consumes an Escape calls
//     preventDefault (and stopPropagation) on it.
//   - An overlay (SlideOver, Modal) listens for Escape on `document` and
//     closes only on an Escape nobody handled first.
//
// So Escape in a picker inside a drawer closes the picker list, and the next
// Escape closes the drawer; the form in the drawer is never dropped by a key
// meant for the list. Pure, so the predicate is unit-tested.

/** The two facts of a keydown the overlay rule reads. */
export interface OverlayKeyFacts {
  key: string;
  defaultPrevented: boolean;
}

/** overlayClosesOn: an overlay closes on Escape unless a control inside it
 *  already handled that Escape (defaultPrevented). */
export function overlayClosesOn(e: OverlayKeyFacts): boolean {
  return e.key === "Escape" && !e.defaultPrevented;
}
