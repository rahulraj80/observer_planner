package main

import (
	"github.com/marmutapp/superbased-observer/internal/config"
)

// init_proxyport.go: lane FIX-UPGRADE D-591 (the orchestrator's ruling on
// D-590 decision 7). `observer init` wrote every proxy route on port 8820,
// the --proxy-port flag's default, even when [proxy].port named another
// port, so the routes it wrote pointed at a port no proxy listens on (the
// upgrade re-test caught it through doctor's proxy.route_port check). The
// route port is now the flag when given, else the config's [proxy].port,
// else 8820: the order `observer org enroll --wire-clients` already used.

// defaultInitProxyPort is the port when neither the flag nor the config
// names one (the daemon's own default).
const defaultInitProxyPort = 8820

// initProxyPortRule is one row of the route-port table.
type initProxyPortRule struct {
	name  string
	match func(flagSet bool, flagPort, configPort int) bool
	pick  func(flagPort, configPort int) int
}

// initProxyPortRules is walked top-down; the first match decides.
var initProxyPortRules = []initProxyPortRule{
	{
		name:  "flag",
		match: func(flagSet bool, flagPort, _ int) bool { return flagSet && flagPort > 0 },
		pick:  func(flagPort, _ int) int { return flagPort },
	},
	{
		name:  "config",
		match: func(_ bool, _, configPort int) bool { return configPort > 0 },
		pick:  func(_, configPort int) int { return configPort },
	},
	{
		name:  "default",
		match: func(bool, int, int) bool { return true },
		pick:  func(int, int) int { return defaultInitProxyPort },
	},
}

// pickInitProxyPort returns the route port and the deciding rule.
func pickInitProxyPort(flagSet bool, flagPort, configPort int) (int, string) {
	for _, r := range initProxyPortRules {
		if r.match(flagSet, flagPort, configPort) {
			return r.pick(flagPort, configPort), r.name
		}
	}
	return defaultInitProxyPort, "default"
}

// configProxyPort is [proxy].port of the config at configPath ("" = the
// default config), or 0 when it cannot be read.
func configProxyPort(configPath string) int {
	cfg, err := config.Load(config.LoadOptions{GlobalPath: configPath})
	if err != nil {
		return 0
	}
	return cfg.Proxy.Port
}

// initProxyPort is the port `observer init` wires into per-tool routes.
func initProxyPort(flagSet bool, flagPort int, configPath string) int {
	p, _ := pickInitProxyPort(flagSet, flagPort, configProxyPort(configPath))
	return p
}
