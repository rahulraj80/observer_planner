// cloudGovernance resolves whether the Settings -> Cloud Intelligence page may
// WRITE on this machine (G11 of the cloud-intelligence parity design).
//
// On an org-managed node the organization pins the `cloud` Settings section
// read-only by default, and the daemon's governance guard then refuses every
// write under /api/cloud/ and /api/config/section/cloud with 409
// governance_read_only. Before this helper the page never learned that, so its
// buttons stayed enabled and failed on click. Every write control on the page
// now reads one state from here, so a read-only section renders its writes
// disabled with the managed explanation instead.
//
// Reads stay available: the network-free evidence Preview ("Show exact bytes")
// is a read-shaped POST the guard exempts from read-only, so it only needs the
// daemon's action routes, never write permission.
//
// Pure (no imports), so `node --test` can exercise it directly.

/** The hover text every locked write control carries (the app-wide wording). */
export const CLOUD_MANAGED_TITLE = "Managed by your organization";

export type CloudWriteState = {
  /** True when a write control on the page may be clicked. */
  writable: boolean;
  /** Why it may not, when the reason is governance; undefined otherwise. */
  lockedTitle?: string;
};

/**
 * cloudWriteState resolves the page's write state from the two facts that gate
 * it: whether the daemon wired the action routes at all, and whether the
 * organization pinned the section read-only. Read-only wins (it names why).
 */
export function cloudWriteState(actionsAvailable: boolean, readOnly: boolean): CloudWriteState {
  if (readOnly) return { writable: false, lockedTitle: CLOUD_MANAGED_TITLE };
  return { writable: actionsAvailable };
}

/**
 * cloudPreviewAvailable reports whether the read-only evidence preview may run:
 * it needs the action routes, and is NOT blocked by a read-only section.
 */
export function cloudPreviewAvailable(actionsAvailable: boolean, _readOnly: boolean): boolean {
  return actionsAvailable;
}
