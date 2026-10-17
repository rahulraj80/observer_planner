// Package otelsink is the self-observability gateway CLIENT: it turns a
// run.DecisionRun into a credential-authenticated OTLP/HTTP span exported to
// the org gateway, behind the neutral emit.Sink contract.
//
// Ownership (modular packaging ruling D-436): this subpackage belongs to the
// export.otel component on the node (with internal/exporter/otel, whose
// tracer provider it builds) and to org.selfobs on the org server. Its
// parent package emit (Sink, Nop) is core, so a build that omits export.otel
// keeps every producer compiling and emits through emit.Nop.
//
// The endpoint is EXACTLY Config.Endpoint: otelsink has NO OTEL_* env
// overrides. New validates the scheme first (only http/https; plaintext http
// only when Insecure), then builds a full explicit-scheme URL so the
// exporter's WithEndpointURL makes the scheme authoritative and an
// OTEL_..._INSECURE env var cannot downgrade the credential-bearing path
// (R5-B1). The sink's HTTP client refuses redirects (so the credential is
// never resent to a redirect target) and applies any Config.TLSConfig.
//
// New returns emit.Nop() (never a nil interface) on an empty endpoint or an
// empty resolved credential. It never imports internal/obs. The tracer name
// and the error prefix stay "selfobs/emit" so exported spans and logs are
// byte-identical to the pre-split sink.
package otelsink
