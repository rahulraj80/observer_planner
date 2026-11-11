//go:build sbo_omit_terminals_sandbox_sandhold || sbo_omit_terminals_sandbox || sbo_omit_isolation || sbo_omit_terminals

package main

import (
	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
)

// The build reports terminals.sandbox.sandhold as compiled out (L1) wherever
// its own omit tag or an ancestor's removes it, as every other L1
// component's omit file does: no sandhold driver registers, so the sandhold
// backend row reads not_in_build, `observer sandbox connect` and
// `disconnect` are absent, and internal/sandboxremote/sandhold is not
// linked.
func init() { components.MarkOmitted("terminals.sandbox.sandhold") }

// sandholdDashboardRoutes is absent with the row: the Terminals >
// Sandboxes tab reads the 404 as "not in this build".
func sandholdDashboardRoutes(string) []dashapi.ExtraRoute { return nil }
