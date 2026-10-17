// Package github holds the pure drivers of GitHub's admin planes (enterprise
// vendor API plan, section 6.3; lane F8 builds the first one).
//
// OrgSeats is the first reference surface end to end:
// github.copilot.org_seats, GET /orgs/{org}/copilot/billing/seats, a
// non-spend snapshot of who holds a Copilot seat. It is written from the
// public GitHub REST description (rest-api-description, ghec.2026-03-10) and
// the GitHub REST pagination and authentication docs, never from a live
// response: the surface is "synthetic, awaiting live validation".
//
// A driver here never touches SQL, logs or HTTP: Build returns a path and a
// query, ParseResponse turns one response into a vendorapi.Page of graph
// observations. The runner (internal/orgserver/vendorconn/runner) owns the
// fetch, the sanitizer (internal/vendorsanitize) the field allowlists and
// the vendorconn store facade every write. imports_test.go pins the package
// pure.
package github
