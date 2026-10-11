package main

import (
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
)

// sshInstances is the instance switcher's SSH forward manager as the terminal
// stack and the dashboard adapter see it: already projected onto the
// dashboard wire shape and the dashboard sentinels. The one implementation
// over internal/sshforward lives in sshinstances_forward.go; a build without
// the terminals.ssh component (-tags sbo_omit_terminals_ssh, lane MP-L1H,
// T-715) gets the disabled one in sshinstances_omit.go, so no core file names
// an internal/sshforward or internal/sshprofile type.
type sshInstances interface {
	// Enabled reports whether the feature is switched on.
	Enabled() bool
	// List returns every configured instance with its live forward state.
	List() []dashapi.InstanceInfo
	// Connect opens (or re-uses) the forward for a profile.
	Connect(name string) (dashapi.InstanceInfo, error)
	// Disconnect closes the forward for a profile.
	Disconnect(name string) error
	// Get returns one row by name.
	Get(name string) (dashapi.InstanceInfo, bool)
	// Test runs the bounded connectivity probe for a configured profile.
	Test(name string) (dashapi.InstanceTestResult, error)
	// TestProfile runs the same probe for an unsaved draft profile.
	TestProfile(p config.SSHProfileConfig) (dashapi.InstanceTestResult, error)
	// Close closes every live forward.
	Close()
}
