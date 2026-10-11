//go:build sbo_omit_ui_dashboard

package main

import (
	"errors"
	"time"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
)

// ui_dashboard_omit.go stands in for ui_dashboard.go and the `observer
// dashboard` verb (dashboard.go) in a build without the node dashboard
// (-tags sbo_omit_ui_dashboard, lane MP-L1M, T-715):
// internal/intelligence/dashboard and its embedded web app are not linked.
//
// Operator rule D-514: a [dashboard] block this build cannot honour never
// stops `observer start`. The daemon runs headless, exactly as with
// --no-dashboard, and says so once ("ui.dashboard is not in this build;
// ..."); `observer dashboard` answers "not in this build" (exit 3); a CLI
// that would print a dashboard link says the dashboard is not in this build
// instead (dashboard_addr.go).
func init() { components.MarkOmitted("ui.dashboard") }

// dashboardInBuild reports whether this binary carries the node dashboard.
const dashboardInBuild = false

// newDashboardCmd registers `observer dashboard` as a compiled-out verb.
func newDashboardCmd() *cobra.Command {
	return compiledOutVerb("dashboard", "Local dashboard (not in this build)", "ui.dashboard")
}

// errDashboardNotInBuild is what the dashboard constructor answers, so a
// caller that reaches it anyway fails with the reason.
var errDashboardNotInBuild = errors.New("the local dashboard (ui.dashboard) is not in this build; " + components.DistrosHaving("ui.dashboard"))

// newDaemonDashboard always refuses: there is no dashboard to build.
func newDaemonDashboard(dashapi.Options) (daemonDashboard, error) {
	return nil, errDashboardNotInBuild
}

// setTerminalPingPolicy has nothing to tune: the terminal websocket bridges
// live in the dashboard.
func setTerminalPingPolicy(time.Duration, time.Duration, int) {}

// checkRemoteBind allows every address: no dashboard listener is bound, so
// nothing can be exposed.
func checkRemoteBind(string, dashapi.RemoteController) error { return nil }
