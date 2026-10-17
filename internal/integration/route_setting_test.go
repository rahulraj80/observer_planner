package integration

import "testing"

// TestRouteSetting pins T-495: the setting a persisted proxy route lives in
// is registry data resolved from the row's shape (an env-settings route's
// EnvVar, or a config-file route's declared Setting), so internal/routeprov
// never names a tool.
func TestRouteSetting(t *testing.T) {
	for _, tc := range []struct {
		tool, want string
	}{
		{"claude-code", "ANTHROPIC_BASE_URL"},
		{"claude-code-windows", "ANTHROPIC_BASE_URL"},
		{"codex", "the Observer model provider (model_provider and [model_providers.openai-observer])"},
		{"codex-windows", "the Observer model provider (model_provider and [model_providers.openai-observer])"},
		{"no-such-tool", ""},
	} {
		if got := RouteSetting(tc.tool); got != tc.want {
			t.Errorf("RouteSetting(%q) = %q, want %q", tc.tool, got, tc.want)
		}
	}
}

// TestRouteSettingFollowsShape: every row's answer comes from its shape, so a
// row without a proxy route has none, a launcher-only route (no persisted
// setting) has none unless it declares one, and an env-settings route names
// its EnvVar.
func TestRouteSettingFollowsShape(t *testing.T) {
	for _, c := range Capabilities() {
		got := RouteSetting(c.Tool)
		switch {
		case c.Proxy == nil:
			if got != "" {
				t.Errorf("%s: no proxy route but RouteSetting = %q", c.Tool, got)
			}
		case c.Proxy.Setting != "":
			if got != c.Proxy.Setting {
				t.Errorf("%s: RouteSetting = %q, want the declared %q", c.Tool, got, c.Proxy.Setting)
			}
		case c.Proxy.Kind == RouteEnvSettings:
			if got != c.Proxy.EnvVar {
				t.Errorf("%s: RouteSetting = %q, want EnvVar %q", c.Tool, got, c.Proxy.EnvVar)
			}
		default:
			if got != "" {
				t.Errorf("%s: %s route with no declared Setting but RouteSetting = %q", c.Tool, c.Proxy.Kind, got)
			}
		}
	}
}
