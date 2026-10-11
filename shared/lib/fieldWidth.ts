// fieldWidth — the ONE rule for who owns a form control's width.
//
// Form controls (shared Input / Select / Textarea, web2's components/ui.tsx
// Input / Select) default to `w-full`. Before this rule they ALWAYS emitted
// `w-full` and appended the caller's className after it; clsx does not
// de-duplicate Tailwind utilities, and `w-full` is generated after `w-auto`
// in the stylesheet, so `className="w-auto"` (every Agent Access filter bar)
// silently lost and each filter rendered full width (UI audit 2026-10-01,
// finding 1.6). Now a caller that sets an UNPREFIXED width utility owns the
// width and the default is dropped. A responsive width (`sm:w-auto`) does not
// count: it composes with the `w-full` base the way Tailwind intends (full
// width on a phone, the caller's width from `sm` up). Pure; unit-tested in
// web2/src/lib/fieldWidth.test.ts.

// An unprefixed width utility: `w-auto`, `w-40`, `w-[220px]`, `!w-56`, at the
// start of the class string or after whitespace (so `min-w-0`, `max-w-xs` and
// `sm:w-auto` never match).
const OWN_WIDTH = /(?:^|\s)!?w-\S/;

/** callerSetsWidth reports whether className carries its own unprefixed width. */
export function callerSetsWidth(className: string | undefined | null): boolean {
  return OWN_WIDTH.test(className ?? "");
}

/** fieldWidthClass is the default width class for a form control given the
 *  caller's className: "w-full", or "" when the caller sets its own width. */
export function fieldWidthClass(className: string | undefined | null): string {
  return callerSetsWidth(className) ? "" : "w-full";
}
