// Package qoder holds the pure drivers of the Qoder Teams OpenAPI (lane
// ENTAPI-L16B, plan Phase 8, T-708): the organization member list.
//
// Written from the public Qoder Teams OpenAPI reference
// (https://docs.qoder.com/account/teams/openapi/members.md and
// .../conventions.md), fetched on 2026-10-08; nothing here was observed from a
// live response. Every request goes to https://api.qoder.com with an
// organization API key as a bearer token, for the organization the
// connection's organization_id parameter names. The surfaces are labelled
// "synthetic, awaiting live validation" in the catalog.
//
// The package is pure (no SQL, HTTP or file system access; imports_test.go
// pins it).
package qoder
