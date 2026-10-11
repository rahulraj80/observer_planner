// Package factory holds the pure drivers of the Factory (droid) Analytics
// API (lane ENTAPI-L16, plan Phase 8, T-619): organization-wide Factory
// Standard Credits and token counts per model per day.
//
// Written from the public Factory Analytics API reference
// (https://docs.factory.com/api-reference/analytics.md), fetched on
// 2026-10-08; nothing here was observed from a live response. Every request
// goes to https://api.factory.ai with a Factory API key (fk-...) as a bearer
// token, held by a Manager or Owner. The surfaces are labelled "synthetic,
// awaiting live validation" in the catalog.
//
// The package is pure (no SQL, HTTP or file system access; imports_test.go
// pins it).
package factory
