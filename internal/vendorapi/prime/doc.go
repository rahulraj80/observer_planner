// Package prime holds the pure drivers of the Prime Intellect platform API
// (lane ENTAPI-L16C, plan Phase 8, T-710): the team member list.
//
// Written from the public Prime Intellect OpenAPI document
// (https://api.primeintellect.ai/openapi.json) and its rendered reference
// (https://docs.primeintellect.ai/api-reference/teams/list-team-members.md),
// fetched on 2026-10-08; nothing here was observed from a live response.
// Every request goes to https://api.primeintellect.ai with an API key as a
// bearer token (OpenAPI security scheme HTTPBearer), for the team the
// connection's team_id parameter names. The surfaces are labelled
// "synthetic, awaiting live validation" in the catalog.
//
// Not built: the per-run usage read (GET /api/v1/billing/runs/{run_id}/usage)
// answers one RFT run by id, and the API documents no listing of runs, so it
// is not an organization read an inventory runner can enumerate; the wallet
// read (balance plus the most recent billing rows) was not reviewed in this
// lane and stays buildable.
//
// The package is pure (no SQL, HTTP or file system access; imports_test.go
// pins it).
package prime
