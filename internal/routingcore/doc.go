// Package routingcore is the pure, always-linked half of model routing
// (docs/model-routing-spec.md): the tier vocabulary and tier table, the
// turn-kind classifier, the command-class resolver, the shared request,
// session and snapshot types, the sub-agent recommendation emitter and the
// counterfactual-replay input types.
//
// It was split out of internal/routing by modular packaging lane MP-L1I
// (T-715, ruling D-512) so that the Model Value Report
// (internal/intelligence/modelvalue), the store's model-value loader and
// the MCP recommendation tools keep working in a build without the
// proxy.routing component (-tags sbo_omit_proxy_routing). The router
// engine, the policy compiler, the bases pipeline, Simulate and the lint
// stay in internal/routing, which re-exports every identifier here
// (core.go) so existing callers compile unchanged.
//
// Like internal/routing it is pure logic: no I/O, no DB, no HTTP, no
// filesystem. Capability flags, not identity: nothing here sees a tool or
// adapter name.
package routingcore
