// Package otelident is the one owner of the OpenTelemetry identity
// vocabulary that more than one plane reads: Observer's echo marker (the
// resource attribute Observer stamps on telemetry it exports itself, so no
// receiver reads it back in) and the hosted-app identity keys (the tenant
// and end-user attributes Plane A, internal/obs, attributes a trace to).
//
// Plane A maps traces with these keys; the Plane B own-agent claim
// (internal/ownagent) uses the very same set to keep a hosted application's
// resource out of the developer plane (PT-55). One list, so the two can
// never disagree about what a hosted app looks like.
//
// The package is data only: no SQL, no HTTP, no fsnotify, no OTel types
// (pinned by otelident_test.go), so every plane, edition and build can import
// it.
package otelident
