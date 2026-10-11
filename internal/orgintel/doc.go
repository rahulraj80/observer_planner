// Package orgintel is the PURE node-side decision layer of org-plane Cloud
// Intelligence parity (docs/plans/cloud-intelligence-parity-design-2026-10-03.md,
// operator rulings R1 / R2).
//
// It answers, from plain inputs the caller resolved at the boundary (the
// enrolment, the cached signed requirement document, the node-authored
// [intelligence].org_enrichment key, the node's own consent record), the
// questions every org-plane surface asks: which class this node is, whether a
// compulsory requirement is PENDING, whether the developer's consent still
// covers what the org asks for, whether org-plane submission is effectively
// on, and what the node reports to its org server. The CLI (`observer intel`),
// the dashboard (/api/intel/*), the daemon's rail gate and the consent report
// all call the same functions, so they can never disagree.
//
// INV-13 lives here as a rule: on an individual-tenancy node the effective
// opt-in needs BOTH the node-authored config key AND a node-side consent
// record; no org document can produce either.
//
// No SQL, no HTTP, no fsnotify (pinned by imports_test.go).
package orgintel
