package dashboard

import "testing"

// TestRouteOwnersT674 pins the routes T-674 moved off core: the
// compression experiments and profile routes belong to proxy.compression,
// so they answer 404 component_not_installed when compression is not
// active, like the rest of the compression page.
func TestRouteOwnersT674(t *testing.T) {
	for route, want := range map[string]string{
		"/api/experiments":                "proxy.compression",
		"/api/experiments/stop":           "proxy.compression",
		"/api/experiments/report":         "proxy.compression",
		"/api/config/profiles":            "proxy.compression",
		"/api/config/profiles/":           "proxy.compression",
		"/api/config/profile-assignments": "proxy.compression",
		"/api/config/section/":            coreComponent,
		"/api/mcp/value":                  "integration.mcp-server",
	} {
		if got := routeComponent(route); got != want {
			t.Errorf("route %q owner = %q, want %q", route, got, want)
		}
	}
}
