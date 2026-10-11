//go:build sbo_omit_ui_dashboard && !sbo_omit_remote

package main

import "github.com/marmutapp/superbased-observer/internal/dashapi"

// newRemoteController returns no controller in a build without the
// dashboard: remote access serves the dashboard, so with nothing to expose
// the remote substrate is never ready and nothing binds beyond loopback.
// The [remote] verbs still read and write their config.
func newRemoteController(dashapi.RemoteOptions) dashapi.RemoteController {
	return nil
}
