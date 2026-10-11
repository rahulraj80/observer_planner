//go:build !sbo_omit_ingest_otlp

package main

import (
	"strings"

	"github.com/marmutapp/superbased-observer/internal/otlpvendor"
)

// otlp_route_marker.go belongs to the ingest.otlp wiring: the marker only
// means something to this node's vendor OTLP ingest, so it leaves the binary
// with it (otlp_route_marker_omit.go is the off side).

// otelResourceAttributesEnv is the OpenTelemetry SDK variable that adds
// attributes to every resource a process exports.
const otelResourceAttributesEnv = "OTEL_RESOURCE_ATTRIBUTES"

// withOTelRouteMarker marks a proxy-routed launch's own telemetry
// (T-536): it appends otlpvendor.RouteResourceAttr=RouteProxy and
// RouteToolResourceAttr=<tool> to OTEL_RESOURCE_ATTRIBUTES (keeping every
// attribute the user already set), so when the tool also exports OTLP to
// this node the ingest knows this run's calls went through the proxy and
// lets the proxy own their tokens. The marker names the launched tool
// (P3-36): a nested tool inheriting the variable is not that tool, so its
// own export is not taken for proxied. A marker inherited from an outer
// launch is replaced, never duplicated.
func withOTelRouteMarker(env []string, tool string) []string {
	route := otlpvendor.RouteResourceAttr + "=" + otlpvendor.RouteProxy
	scoped := otlpvendor.RouteToolResourceAttr + "=" + tool
	cur, _ := lookupEnvValue(env, otelResourceAttributesEnv)
	var keep []string
	for _, kv := range strings.Split(cur, ",") {
		k := strings.TrimSpace(kv)
		if k == "" {
			continue
		}
		if name, _, _ := strings.Cut(k, "="); strings.TrimSpace(name) == otlpvendor.RouteResourceAttr ||
			strings.TrimSpace(name) == otlpvendor.RouteToolResourceAttr {
			continue
		}
		keep = append(keep, k)
	}
	val := strings.Join(append(keep, route, scoped), ",")
	out := make([]string, 0, len(env)+1)
	prefix := otelResourceAttributesEnv + "="
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return append(out, prefix+val)
}
