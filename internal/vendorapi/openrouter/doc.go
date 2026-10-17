// Package openrouter holds the pure drivers of the OpenRouter management API
// (lane ENTAPI-L16C, plan Phase 8, T-710): the organization member list, the
// activity report and the credits balance.
//
// Written from the public OpenRouter API reference
// (https://openrouter.ai/docs/api/api-reference/organization/list-organization-members.md,
// .../analytics/get-user-activity-grouped-by-endpoint.md and
// .../credits/get-remaining-credits.md), fetched on 2026-10-08; nothing here was
// observed from a live response. Every request goes to https://openrouter.ai
// under /api/v1 with a management key as a bearer token (OpenAPI security
// scheme apiKey: http bearer).
//
// OpenRouter is a gateway route with no registry adapter: its activity lands
// as UNATTRIBUTED gateway facts under vendorapi.GatewayTool("openrouter"),
// never under a registry tool (see vendorapi/gateway.go). The surfaces are
// labelled "synthetic, awaiting live validation" in the catalog.
//
// The package is pure (no SQL, HTTP or file system access; imports_test.go
// pins it).
package openrouter
