// Package google holds the pure drivers of the Google Cloud surfaces
// (enterprise vendor API plan, Phase 8; lane ENTAPI-L16D, T-713). Gemini Code
// Assist and the Gemini CLI on a Standard or Enterprise license have no admin
// usage API: their organization record is the cloudaicompanion API's Cloud
// Audit Logs in the customer's Google Cloud project, read through the Cloud
// Logging API (entries.list) with a service account token from the egress
// seam's reviewed JWT bearer step.
//
// The filter is never written by a caller: vendorapi.CloudAuditLogQuery
// composes it from the project id and a closed service table. The audit
// entry's request and response payloads are never decoded (closed DTO). The
// package is pure: no SQL, no HTTP, no file system (imports_test.go). Nothing
// here was observed from a live response; every surface stays "synthetic,
// awaiting live validation".
package google
