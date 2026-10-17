// Package microsoft holds the pure drivers of the Microsoft 365 Copilot
// usage reports in Microsoft Graph (enterprise vendor API plan, section 6.3;
// lane ENTAPI-D3, task T-692, plan lane L9).
//
// Three surfaces are built, all written from Microsoft's public reference
// (MicrosoftDocs/m365copilot-docs, the copilotReportRoot pages, fetched
// 2026-10-08), never from a live response, so all three are "synthetic,
// awaiting live validation":
//
//   - microsoft.m365.reports_usage_user_detail:
//     getMicrosoft365CopilotUsageUserDetail, one snapshot per licensed user
//     of the per-app last-activity dates; OData @odata.nextLink paging.
//   - microsoft.m365.reports_user_count_summary:
//     getMicrosoft365CopilotUserCountSummary, enabled and active users per
//     app over the report period, as org facts.
//   - microsoft.m365.reports_user_count_trend:
//     getMicrosoft365CopilotUserCountTrend, enabled and active users per app
//     per day, as org facts.
//
// All three authenticate with the OAuth 2.0 client-credentials grant
// (application permission Reports.Read.All): the egress seam's reviewed
// token step posts the client id, the fixed Graph scope and the client
// secret to the catalog token host (login.microsoftonline.com) and sends the
// access token as a bearer token to graph.microsoft.com. The connection
// parameters tenant_id and client_id name the app registration; the client
// secret is the sealed auth binding secret.
//
// The reports are requested on the v1.0 path with $format=application/json.
// Microsoft documents the JSON representation on the beta pivot of the same
// pages and CSV as the v1.0 default; that the v1.0 path answers the same JSON
// under $format is Inferred, so every field ledger grade is Inferred and the
// rungs stop at assumption_tested.
//
// A driver here never touches SQL, logs or HTTP. imports_test.go pins the
// package pure.
package microsoft
