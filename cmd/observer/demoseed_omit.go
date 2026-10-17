//go:build sbo_omit_demo

package main

import "github.com/spf13/cobra"

// newDemoSeedCmd is demoseed.go's twin in a -tags sbo_omit_demo build:
// internal/demo is not linked, so `observer demoseed` answers "not in this
// build".
func newDemoSeedCmd() *cobra.Command {
	return compiledOutVerb("demoseed", "Seed a synthetic multi-user week (not in this build)", "demo")
}
