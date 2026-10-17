// Package openai holds the pure drivers of the OpenAI Platform Admin API
// (enterprise vendor API plan, section 6.3 and Phase 5 "OpenAI activity";
// lane ENTAPI-D1, task T-610, plan lane L7).
//
// Four surfaces are built, all written from the official openai-python admin
// types and parameters (src/openai/types/admin/organization, fetched
// 2026-10-07 and 2026-10-08) and the fixture corpus synthesized from them,
// never from a live response, so every one is "synthetic, awaiting live
// validation" and rests at most on assumption_tested (each corpus page has an
// Inferred envelope field):
//
//   - openai.platform.usage_completions: GET /v1/organization/usage/completions,
//     daily buckets grouped by model, landed as aggregate token facts.
//   - openai.platform.costs: GET /v1/organization/costs, daily buckets grouped
//     by line item, landed as USD facts (the vendor reports dollars).
//   - openai.platform.audit_logs: GET /v1/organization/audit_logs, landed as
//     vendor audit events.
//   - openai.platform.users: GET /v1/organization/users, the organization's
//     members as User graph resources.
//
// The Platform usage and cost APIs have no Codex dimension (EA-F11): their
// facts are the org's whole API traffic, attributed to the codex tool only as
// a segment, never as complete coverage. The legacy codexanalytics poller's
// openai_org surface reads the same two endpoints, so the runner runs them
// only after the codexanalytics.openai_org ownership moved to the
// connection; that one move stops the legacy reads of both, so both
// connections should exist before it.
//
// Every catalog row is classified may_return_secret, so every driver here is
// closed-DTO: it decodes into closed types and reads no other member (an
// audit event's per-type detail object is read for its id only). Every
// surface authenticates with an Admin key as a bearer token.
//
// A driver here never touches SQL, logs or HTTP: Build returns a path and a
// query, ParseResponse turns one response into a vendorapi.Page.
// imports_test.go pins the package pure.
package openai
