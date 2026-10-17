// Package kilo holds the pure drivers of the Kilo Code API (lane
// ENTAPI-L16C, plan Phase 8, T-710): one reviewed tRPC request encoder and
// response unwrapper (trpc.go), the organization-wide daily token series and
// the organization member list.
//
// Written from the public Kilo Code OpenAPI document
// (https://app.kilo.ai/api/openapi.json), fetched on 2026-10-08; nothing here
// was observed from a live response. Every request goes to https://app.kilo.ai
// with an API key as a bearer token (OpenAPI security scheme bearerAuth).
//
// Not built: usageAnalytics.getSummary (its answer carries no period, so a
// window total cannot be keyed to a day from the response alone), getTable and
// getBreakdown (window totals per dimension with a limit and no cursor, the
// same period problem plus truncation), and the series' cost metric (its unit
// is not documented). Kilo Gateway usage is gateway traffic and lands under
// the reserved attribution vendorapi.GatewayTool("kilo"), never under a
// registry tool. The surfaces are labelled "synthetic, awaiting live
// validation" in the catalog.
//
// The package is pure (no SQL, HTTP or file system access; imports_test.go
// pins it).
package kilo
