// helpPostureView: help wording that depends on how this machine is enrolled
// (T-470, D-007).
//
// The registry in help.ts is written for an individual machine, where every
// sharing switch is the developer's own and an organization can only lower or
// lock it. On an organization-managed machine that is false: the org ships
// full content by its enrolment grant (D-007, CLAUDE.md "Enterprise / org-
// managed nodes ship full content"). Help that says otherwise there is a false
// assurance, so the entries whose truth depends on the posture carry a managed
// wording here, the same override shape helpTokenView.ts uses for the
// token-usage view. Pure: no React, no DOM.

import type { HelpEntry } from "./help.ts";
import type { HelpTextFields } from "./helpTokenView.ts";

/** The enrolment posture help is written for. */
export type HelpPosture = "individual" | "managed";

/** helpPostureOf maps the governance read's `managed` flag to a posture. An
 *  absent flag (a solo node, a pre-managed daemon, a posture still loading)
 *  is the individual default. */
export function helpPostureOf(managed: boolean | undefined): HelpPosture {
  return managed ? "managed" : "individual";
}

export const HELP_MANAGED_VIEW: Readonly<Record<string, HelpTextFields>> = {
  "glossary.settings_org_share": {
    oneLiner:
      "What this organization-managed machine shares: full content (raw commands, prose and paths alongside the hashes) ships by your organization's enrolment grant, not by a switch you flip here.",
    detail:
      "This machine enrolled under organization-managed tenancy, so your organization decides what it shares. Enrolment set share.full_content = true in this node's config.toml (a machine enrolled before that default is raised to full content by the organization's signed enrolment grant), so raw commands, assistant prose and filesystem paths ship alongside the sha256 hashes, visible to your org admins. The organization can also raise the metadata reporting tiers (routing_summary, for example) and can deliberately lower sharing again through its signed policy.\n\nNone of it is hidden from you: the Privacy page lists every sharing key, what is in force and who decided it (you, or your organization with its policy version), and shows the byte-for-byte payload of the last push. Switches your organization manages may be read-only or hidden in this section.\n\nEnrolment itself (server URL, credentials) is owned by the Enrolment section - a save here can never detach or re-point it (test-pinned). Push-loop changes bind at daemon start: restart to apply.",
  },
  "card.privacy_org_push": {
    detail:
      "This machine is organization-managed, so its pushes carry full content - raw commands, prose and paths alongside the hashed identifiers - by your organization's enrolment grant, unless the organization has deliberately lowered it. The payload button fetches the exact JSON of the most recent push, byte-for-byte, so 'what left this machine' is verifiable rather than asserted.",
  },
};

/** applyPostureView resolves one entry's wording for a posture: identity on
 *  an individual machine, the managed override's fields where present on a
 *  managed one. */
export function applyPostureView(e: HelpEntry, posture: HelpPosture): HelpEntry {
  if (posture !== "managed") return e;
  const o = HELP_MANAGED_VIEW[e.id];
  if (!o) return e;
  return { ...e, ...o };
}
