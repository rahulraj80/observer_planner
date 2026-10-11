// The Requests page's line about this machine's org exceptions (review
// M4-09, M4-14). Pure (no React) so grantsCache.test.ts runs under
// `node --test`.
//
// It never says "Confirmed" about a grant document that no longer verifies
// on this machine, and it names the organization through orgDisplayLabel
// (T-160), never a raw org_name that may be an org id.

import { orgDisplayLabel } from "./orgName.ts";

export type GrantsCacheFacts = {
  /** grants_cached: the node holds a grant document for this enrolment. */
  cached: boolean;
  /** grants.unverified: that document no longer verifies here. */
  unverified?: boolean;
  lastVerifiedNs: number;
  maxOfflineS: number;
  orgName?: string | null;
  nowNs: number;
};

/** grantsCacheLine is the card subtitle; duration renders whole seconds. */
export function grantsCacheLine(f: GrantsCacheFacts, duration: (seconds: number) => string): string {
  const org = orgDisplayLabel(f.orgName);
  if (!f.cached) return "Nothing confirmed with the organization yet.";
  if (f.unverified) {
    return `The exceptions this machine holds from ${org} no longer verify here, so none of them is in force. They are checked again on the next sync.`;
  }
  const ago = f.lastVerifiedNs ? " " + duration(Math.round((f.nowNs - f.lastVerifiedNs) / 1e9)) + " ago" : "";
  return `Confirmed with ${org}${ago}. Offline, an exception holds for at most ${duration(f.maxOfflineS || 600)} after the last confirmation.`;
}

/** sentToLine is the "Your requests" subtitle. */
export function sentToLine(orgName: string | null | undefined, requestsError?: string): string {
  return requestsError ?? `Sent to ${orgDisplayLabel(orgName)}. Admins answer from their Requests queue.`;
}
