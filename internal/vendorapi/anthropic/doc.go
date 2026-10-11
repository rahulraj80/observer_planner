// Package anthropic holds the pure drivers of Anthropic's Claude Console
// Admin API (enterprise vendor API plan, section 6.3 and Phase 5 "Anthropic
// activity"; lane ENTAPI-D1, task T-609, plan lane L6).
//
// Four surfaces are built, all written from the public Admin API reference
// and guides (platform.claude.com, fetched 2026-10-07 and 2026-10-08) and
// the fixture corpus's verbatim reference examples, never from a live
// response, so every one is "synthetic, awaiting live validation":
//
//   - anthropic.console.usage_messages: GET /v1/organizations/usage_report/messages,
//     daily buckets grouped by model, landed as aggregate token facts.
//   - anthropic.console.cost_report: GET /v1/organizations/cost_report, daily
//     buckets grouped by description, landed as USD facts (the vendor reports
//     a decimal string in cents).
//   - anthropic.console.claude_code_usage: GET /v1/organizations/usage_report/claude_code,
//     one record per actor per day per is_remote, landed as per-identity facts
//     (tokens, estimated cost, sessions, lines, commits, pull requests, tool
//     accept and reject counts). The legacy ccanalytics poller (api_kind admin)
//     reads the same report, so the runner runs this surface only after the
//     ccanalytics.admin ownership moved to the connection.
//   - anthropic.console.users: GET /v1/organizations/users, the organization's
//     members as User graph resources.
//
// One Claude Enterprise surface is built the same way (lane ENTAPI-H, T-697):
//
//   - anthropic.enterprise.analytics.user_cost_report: GET
//     /v1/organizations/analytics/user_cost_report, daily buckets grouped by
//     product, landed as per-identity USD facts (cost and list cost) and
//     request counts for seat users.
//
// Every Console surface authenticates with the Admin API key in the X-Api-Key
// header (the raw-header scheme) and sends anthropic-version: 2023-06-01.
// Workspace keys do not work on these endpoints, so a 401 or 403 ends the
// run. The Enterprise surface sends an Analytics API key (scope
// read:analytics) in the same header.
//
// A driver here never touches SQL, logs or HTTP: Build returns a path and a
// query, ParseResponse turns one response into a vendorapi.Page. The runner
// owns the fetch, the sanitizer the allowlists and the vendorconn store
// facade every write. imports_test.go pins the package pure.
package anthropic
