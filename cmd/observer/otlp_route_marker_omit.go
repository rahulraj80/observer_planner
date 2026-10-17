//go:build sbo_omit_ingest_otlp

package main

// otlp_route_marker_omit.go is the off side of otlp_route_marker.go: with no
// vendor OTLP ingest in this build there is nothing to tell that a launch was
// proxied, so the launched tool's environment is left exactly as given.
func withOTelRouteMarker(env []string, _ string) []string { return env }
