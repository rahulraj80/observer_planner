// Package vendorsurface holds the pure types of the Vendor Connections surface
// catalog (enterprise vendor API plan, docs/plans/enterprise-vendor-api-plan-2026-10-07.md,
// sections 4.0, 4.1 and 4.5; lane F1, task T-603).
//
// A Surface is one vendor admin plane reached over one transport with one auth
// contract (for example "cursor.admin.usage_events"). It declares what it
// supplies (data classes with grain, unit, accuracy, coverage), which control
// classes it reads or writes (with independent Read, Write, Readback, local-file
// and repository facets), how it is reached, what it costs in secrets
// (SecretClass) and sensitivity, its build state, and the sources it was
// researched from. The catalog rows themselves live in the catalog subpackage.
//
// Verification is data and is honest by construction: every supply and control
// carries a synthetic rung (doc_only, contract_fixtures, assumption_tested,
// mock_verified) and a live rung, and the catalog may never claim a live rung
// beyond live_pending; live validation is recorded per connection in the
// database by a later lane. Every row carries the status label
// "synthetic, awaiting live validation" (StatusLabel).
//
// This package is pure: no database/sql, net/http, os/exec or fsnotify
// (imports_test.go), and it never imports internal/integration (invariant I2:
// the tool to surface relation is authored only there; validators here take the
// canonical tool set as an input when they need it).
package vendorsurface
