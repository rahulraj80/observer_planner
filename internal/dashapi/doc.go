// Package dashapi is the core vocabulary the node dashboard shares with the
// rest of the node: the dashboard's Options and the seam interfaces it is
// wired through (launch, terminal status, remote access, enrolment, org
// intelligence, components, updates, tool installs), the request and result
// rows its routes and the CLI both speak, the sentinel errors callers match,
// and the three compositions a CLI verb also runs (ComposeProjectDetail,
// ComposeProjectSkills, ComputeExperimentReport).
//
// It exists so cmd/observer can wire the daemon without importing
// internal/intelligence/dashboard (modular packaging, lane MP-L1M, T-715):
// a build with -tags sbo_omit_ui_dashboard leaves the dashboard package and
// its embedded web app out of the binary while every other component keeps
// compiling against these types. The dashboard package re-exports each
// exported name here (type X = dashapi.X), so its own code and its callers
// are unchanged.
//
// This package is core: it imports no component-owned package outside a
// file tagged for that component (remote_controller.go, shellwrap.go and
// their omit twins), and it owns no HTTP handler, listener or goroutine.
package dashapi
