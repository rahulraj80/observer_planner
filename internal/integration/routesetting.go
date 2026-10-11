package integration

import "strings"

// RouteSetting is the human name of the setting tool's persisted proxy route
// writes ("ANTHROPIC_BASE_URL", or a config-file route's declared
// ProxyRoute.Setting), or "" when the registry grounds none. A "-windows"
// cross-OS target reads its tool's row. Resolved from the row's shape, never
// the tool name (T-495: internal/routeprov's Leave message used to switch on
// the tool to name it).
func RouteSetting(tool string) string {
	c, ok := For(strings.TrimSuffix(tool, "-windows"))
	if !ok || c.Proxy == nil {
		return ""
	}
	if c.Proxy.Setting != "" {
		return c.Proxy.Setting
	}
	if c.Proxy.Kind == RouteEnvSettings {
		return c.Proxy.EnvVar
	}
	return ""
}
