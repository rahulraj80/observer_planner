// Package cognition holds the pure drivers of the Cognition Devin v3
// enterprise API (lane ENTAPI-L16, plan Phase 8, T-619): daily ACU
// consumption, the enterprise member list and the enterprise audit log.
//
// Written from the public Devin v3 OpenAPI description
// (https://docs.devin.ai/v3-openapi.yaml) and its reference pages, fetched on
// 2026-10-08; nothing here was observed from a live response. Every request
// goes to https://api.devin.ai with a service-user credential as a bearer
// token (the description's only security scheme). The surfaces are labelled
// "synthetic, awaiting live validation" in the catalog.
//
// The package is pure (no SQL, HTTP or file system access; imports_test.go
// pins it): Build returns a vendorapi.Request and Parse reads a body.
package cognition
