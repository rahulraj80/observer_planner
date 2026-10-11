//go:build !sbo_omit_ui_dashboard && !sbo_omit_remote

package main

import (
	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/intelligence/dashboard"
)

// newRemoteController builds the concrete remote-access controller the
// dashboard serves through (ui.dashboard with remote; see ui_dashboard.go).
func newRemoteController(o dashapi.RemoteOptions) dashapi.RemoteController {
	return dashboard.NewRemoteController(o)
}
