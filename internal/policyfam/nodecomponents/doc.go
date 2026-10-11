// Package nodecomponents is the PURE node.components family foundation
// (modular packaging plan docs/plans/modular-packaging-plan-2026-10-07.md
// section 6.2, lane MP-GOVF): the org's signed answer to "which components
// may run on this managed node".
//
// What lives here:
//
//   - the v1 wire body (BodyV1) with its strict decoder and canonical
//     encoding (deny entries are typed {selector, grantable} objects, sorted
//     by selector, grantable omitted when false);
//   - the selector grammar (exact id, prefix glob "adapter.*", "class:<c>",
//     "stability:<s>") and the most-specific-wins reduction of a body to
//     exact id sets (PolicySpec.Sets);
//   - the table that picks which policy is in force on a node (Govern:
//     individual, dormant offboarded enrolment, current signed policy,
//     last-known-good, the enrolment grant's initial_components, or the
//     legacy pin for a managed node enrolled before this family existed);
//   - the legacy update-state defaults the agent migration backfills.
//
// What does NOT live here, by design: the resolver. Govern returns a
// components.Governance, and components.Resolve applies it through rows of
// its one resolveRules table (never a second resolver). Delivery on the
// policy-resource rail, the components.govern authority in internal/govern,
// publish-time lint across rails and the web2 composer are lane MP-GOV's;
// the local ceiling is read by internal/managedconfig.
//
// Purity (CLAUDE.md #1): no database/sql, net/http, fsnotify, os,
// internal/config, internal/obs or internal/proxy, pinned by
// imports_test.go.
package nodecomponents
