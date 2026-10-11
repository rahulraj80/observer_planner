// Package perplexity holds the pure drivers of the Perplexity organization
// Analytics API (the perplexity-web adapter's vendor; lane ENTAPI-L16B, plan
// Phase 8, T-708): per-member daily credit usage.
//
// Written from the public OpenAPI reference
// (https://docs.perplexity.ai/api-reference/computer-analytics-usage-v2-get.md),
// fetched on 2026-10-08; nothing here was observed from a live response.
// Every request goes to https://api.perplexity.ai with the organization's
// analytics API key (admin-generated, analytics-only) as a bearer token. The
// surfaces are labelled "synthetic, awaiting live validation" in the catalog.
//
// The package is pure (no SQL, HTTP or file system access; imports_test.go
// pins it).
package perplexity
