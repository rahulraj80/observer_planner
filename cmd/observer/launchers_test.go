package main

import (
	"sort"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/integration"
)

// TestLauncherRegistryCoversEveryLaunchSubcommand keeps the launcher table
// (launchers.go) and the integration registry in step: every launcher verb
// is some registry row's Handoff.Launch.Subcommand (so the verb gate owns it
// through an adapter component and an inactive or compiled-out adapter's
// launcher refuses), no verb is listed twice, and every registry launch
// subcommand is a verb of the root command.
func TestLauncherRegistryCoversEveryLaunchSubcommand(t *testing.T) {
	subs := map[string]string{}
	for _, tool := range integration.Tools() {
		c, ok := integration.For(tool)
		if !ok || c.Handoff.Launch == nil || c.Handoff.Launch.Subcommand == "" {
			continue
		}
		if _, taken := subs[c.Handoff.Launch.Subcommand]; !taken {
			subs[c.Handoff.Launch.Subcommand] = tool
		}
	}
	owners := verbComponents()
	seen := map[string]bool{}
	for _, mk := range launcherCommands {
		name := mk().Name()
		if seen[name] {
			t.Errorf("launcher %s is listed twice", name)
		}
		seen[name] = true
		tool, ok := subs[name]
		if !ok {
			t.Errorf("launcher %s is no registry row's launch subcommand", name)
			continue
		}
		if owners[name] != components.AdapterID(tool) {
			t.Errorf("launcher %s is owned by %q, want %s", name, owners[name], components.AdapterID(tool))
		}
	}
	root := map[string]bool{}
	for _, c := range newRootCmd().Commands() {
		root[c.Name()] = true
	}
	var missing []string
	for sub := range subs {
		if !root[sub] {
			missing = append(missing, sub)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("registry launch subcommands with no root verb: %v", missing)
	}
}
