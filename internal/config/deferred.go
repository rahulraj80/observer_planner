package config

import "strings"

// deferred.go is the config side of operator rule D-514: a setting that
// names a feature this build does not have yet must never stop `observer
// start`. Load switches each such setting off on the loaded copy (never in
// the file) and records one reason; Validate therefore never sees it. The
// file keeps the operator's value, so a later build that has the feature
// honours it without an edit.

// deferredSetting is one row of the table: when on reports the setting is
// asked for, off switches it off, and reason is the one line shown.
type deferredSetting struct {
	on     func(*Config) bool
	off    func(*Config)
	reason string
}

// deferredSettings is walked top-down (CLAUDE.md module rule 5).
var deferredSettings = []deferredSetting{
	{
		on:     func(c *Config) bool { return c.Routing.Translation.Enabled },
		off:    func(c *Config) { c.Routing.Translation.Enabled = false },
		reason: "routing.translation.enabled is off: cross-provider translation (a P3 track, section R11.4) is not in this build; enforce mode keeps same-provider-shape candidates. Fix: set [routing.translation].enabled = false to silence this.",
	},
	{
		on:     func(c *Config) bool { return c.Guard.Rules.CEL },
		off:    func(c *Config) { c.Guard.Rules.CEL = false },
		reason: "guard.rules.cel is off: CEL user rules are not in this build (matchers v1); the matcher rules still run. Fix: set [guard.rules].cel = false to silence this.",
	},
	{
		on: func(c *Config) bool {
			return c.Remote.Enabled && strings.EqualFold(strings.TrimSpace(c.Remote.Mode), "lan")
		},
		off:    func(c *Config) { c.Remote.Mode = "off" },
		reason: "remote.mode \"lan\" is off: LAN remote access is not in this build (tailnet HTTPS only, operator decision 2026-07-12); the dashboard stays on loopback. Fix: set [remote].mode = \"tailscale\" (Settings > Remote).",
	},
}

// deferUnsupported switches off every setting in deferredSettings that c
// asks for and returns their reasons, in table order.
func deferUnsupported(c *Config) []string {
	var out []string
	for _, d := range deferredSettings {
		if d.on(c) {
			d.off(c)
			out = append(out, d.reason)
		}
	}
	return out
}
