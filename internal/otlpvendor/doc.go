// Package otlpvendor is the vendor-agnostic, table-driven mapping from an AI
// coding tool's own OpenTelemetry export to Plane B observations: per-call
// token turns, tool calls, captured content and session sightings.
//
// The node's OTLP receiver (internal/ingest/otlp) is schema-blind; this
// package is the ONE place that knows what each vendor's export looks like.
// Every vendor is one row in the Vendors table: a resource match (the
// exporter's service.name), and an ordered list of per-signal rules, each
// naming the event (log record event.name) or span it matches and the
// attribute keys that carry the session id, model, token counts, tool call
// and content. Decision logic walks the table top-down; there is no switch
// on tool identity anywhere in the parse path (CLAUDE.md module rules 3/5).
//
// Token figures are normalized AT THIS BOUNDARY to the proxy-row convention
// the store already deduplicates on (internal/sessionmsg twin folding):
// input is NET of cache reads (and of cache writes where the vendor counts
// them inside input), output is GROSS of reasoning. A row's per-vendor
// Normalize flags say which conversion its export needs, and a fixture test
// per vendor pins it.
//
// The package is pure: it reads already-decoded OTLP proto messages and
// returns plain structs. No database/sql, no net/http, no fsnotify (pinned
// by imports_test.go). The daemon glue in cmd/observer/otlp_ingest.go turns
// the result into store writes through the existing seams; Plane A
// (internal/obs) is never imported here and never sees a resource a vendor
// row claimed.
package otlpvendor
