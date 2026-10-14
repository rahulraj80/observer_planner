// Package planlimits is the pure core of the "Plans & limits" module: how much
// of each AI subscription plan and API balance a developer has left
// (docs/plans-and-limits.md; plan of record: the Phase 0 results doc
// docs/plans/plans-and-limits-phase0-2026-10-13.md and operator rulings
// D-640 and D-642).
//
// It owns, as data and pure functions:
//
//   - the normalized model (Snapshot, Window, Balance, Spend), where a nil
//     number always means "unknown" and never zero;
//   - the provider and source registry (Providers, Sources), the closed
//     source-kind to rung table (RungOf) and the ladder rule that decides
//     whether an experimental source may run (ShouldPoll);
//   - the per-source drivers: request planning and response parsing for every
//     official and experimental source, as pure functions over bytes
//     (Drivers);
//   - the passive mappings over data Observer already captures (proxy
//     limit_snapshots rows, the Codex log rate_limits envelope, the Claude
//     Code status line rate_limits block) and the read-time account-scope
//     binding rule table (ResolveScope);
//   - merge, view building, freshness, pace, cadence, backoff and the
//     suspicious-reset hold.
//
// Purity: no database/sql, net/http, os/exec or fsnotify (imports_test.go).
// Every network call lives in internal/planlimitsnet, every read of a vendor
// tool's local file in internal/planlimitslocal, every SQL statement in
// internal/store/planlimits.go, and the polling loop in
// internal/planlimitssvc. Secrets reach this package only as in-memory values
// inside a PlanInput and leave it only inside Request.SecretHeaders, which no
// caller logs or stores.
package planlimits
