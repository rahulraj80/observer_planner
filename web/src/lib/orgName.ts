// Organization display name: the ONE web owner of what the node dashboard
// calls the developer's organization (T-160). Every surface (the enrolment
// page, the governance "managed by" / "decided by" strings, the Privacy card,
// the top-bar badge, Cloud Intelligence) goes through orgDisplayLabel, so the
// fallback copy and the never-an-org-id rule cannot drift between them.
//
// The daemon already picks the name (orgenrol.DisplayName: a real name, else
// the org server's host, never an org id) and serves it as `org_display` /
// `org_name`; this helper only chooses among the candidates it was handed and
// supplies the fallback. Pure: no React, so orgName.test.ts runs under
// `node --test`.

/** ORG_NAME_FALLBACK is what every surface says when no name is known. */
export const ORG_NAME_FALLBACK = "your organization";

// An org id is 32 hex characters. A grant recorded before the display-name fix
// carries the id where the name belongs; the developer must never see it.
const ORG_ID_SHAPE = /^[0-9a-f]{32}$/i;

/**
 * orgDisplayLabel names the developer's organization: the first candidate
 * that is a real name (non-empty, not id-shaped), else ORG_NAME_FALLBACK.
 * Use it mid-sentence ("Enrolled in <label> as ...").
 */
export function orgDisplayLabel(...candidates: (string | null | undefined)[]): string {
  for (const c of candidates) {
    const name = (c ?? "").trim();
    if (name && !ORG_ID_SHAPE.test(name)) return name;
  }
  return ORG_NAME_FALLBACK;
}

/**
 * orgDisplayLabelSentence is orgDisplayLabel for the start of a sentence or a
 * table cell: only the fallback is capitalised, a real name is left as is.
 */
export function orgDisplayLabelSentence(...candidates: (string | null | undefined)[]): string {
  const label = orgDisplayLabel(...candidates);
  return label === ORG_NAME_FALLBACK ? "Your organization" : label;
}
