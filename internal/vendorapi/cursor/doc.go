// Package cursor holds the pure drivers of Cursor's Admin API (enterprise
// vendor API plan, section 6.3; lane F11, task T-608, plan lane L5).
//
// Two surfaces are built, both written from the public Cursor Admin API
// reference (cursor.com/docs/account/teams/admin-api, fetched 2026-10-07),
// never from a live response, so both are "synthetic, awaiting live
// validation":
//
//   - cursor.admin.members: GET /teams/members, a snapshot of the team's
//     members (User graph resources: id, name, email, role, removed flag).
//   - cursor.admin.audit_logs: GET /teams/audit-logs, the team audit log as
//     vendor audit events (page-number paging in the query, a 30-day
//     maximum window per request).
//
// Both authenticate with HTTP Basic, the team API key as the user name and
// an empty password. The read-query POST surfaces (filtered usage events,
// daily usage, spend) are not built: the egress seam sends GET only.
//
// A driver here never touches SQL, logs or HTTP: Build returns a path and a
// query, ParseResponse turns one response into a vendorapi.Page. The runner
// owns the fetch, the sanitizer the allowlists and the vendorconn store
// facade every write. imports_test.go pins the package pure.
package cursor
