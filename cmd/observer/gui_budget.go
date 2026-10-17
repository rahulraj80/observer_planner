package main

import "github.com/marmutapp/superbased-observer/internal/integration"

// gui_budget.go holds the GUI launch budget evidence shared by the dashboard
// GUI spawn, `observer ide` and the shell-wrap bridge (lane MP-L1K, T-715,
// moved out of gui_launch.go so the shell-wrap bridge keeps it in a build
// without the terminals component).

// guiBudgetEvidence is the managed-budget admission evidence for a GUI launch:
// the route stays unknown (a GUI routing hint does not prove which provider an
// IDE's extensions select) and the surface class is the registry's declared
// class for the row. Shared by the dashboard spawn and `observer ide` so both
// admit a GUI launch identically.
func guiBudgetEvidence(id string) budgetLaunchEvidence {
	evidence := budgetLaunchEvidence{Route: budgetLaunchRouteUnknown}
	if class, ok := integration.GUILaunchSurfaceClass(id); ok {
		evidence.SurfaceClass = class
	}
	return evidence
}
