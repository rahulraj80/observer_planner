// Package xai holds the pure drivers of the xAI Management API (the grok
// adapter's vendor; lane ENTAPI-L16B, plan Phase 8, T-708): the team audit
// events and the postpaid spending limits.
//
// Written from the public Management API guide and reference
// (https://docs.x.ai/developers/management-api-guide.md,
// https://docs.x.ai/developers/rest-api-reference/management/audit.md,
// https://docs.x.ai/developers/rest-api-reference/management/billing.md),
// fetched on 2026-10-08; nothing here was observed from a live response.
// Every request goes to https://management-api.x.ai with a management key
// (separate from the inference API key) as a bearer token, for the team the
// connection's team_id parameter names. The surfaces are labelled
// "synthetic, awaiting live validation" in the catalog.
//
// The package is pure (no SQL, HTTP or file system access; imports_test.go
// pins it).
package xai
