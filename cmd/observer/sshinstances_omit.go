//go:build sbo_omit_terminals_ssh && !sbo_omit_terminals

package main

import (
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
)

// sshinstances_omit.go stands in for ssh.go and sshinstances_forward.go in a
// build without the terminals.ssh component (-tags sbo_omit_terminals_ssh,
// lane MP-L1H, T-715): internal/sshforward and internal/sshprofile are not
// linked. `observer ssh` answers "not in this build" (exit 3), an SSH launch
// from the dashboard is refused before a run is minted, and the instance
// switcher reports the feature disabled.
func init() { components.MarkOmitted("terminals.ssh") }

// newSSHCmd registers `observer ssh` as a compiled-out verb.
func newSSHCmd() *cobra.Command {
	return compiledOutVerb("ssh", "SSH terminals (not in this build)", "terminals.ssh")
}

// newSSHInstances returns the disabled instance switcher.
func newSSHInstances(*terminalConfigHolder, *slog.Logger) sshInstances { return noSSHInstances{} }

// noSSHInstances is the instance switcher in a build without terminals.ssh:
// disabled, empty, and every verb answers ErrInstancesDisabled.
type noSSHInstances struct{}

func (noSSHInstances) Enabled() bool                { return false }
func (noSSHInstances) List() []dashapi.InstanceInfo { return nil }
func (noSSHInstances) Connect(string) (dashapi.InstanceInfo, error) {
	return dashapi.InstanceInfo{}, dashapi.ErrInstancesDisabled
}
func (noSSHInstances) Disconnect(string) error { return dashapi.ErrInstancesDisabled }
func (noSSHInstances) Get(string) (dashapi.InstanceInfo, bool) {
	return dashapi.InstanceInfo{}, false
}
func (noSSHInstances) Test(string) (dashapi.InstanceTestResult, error) {
	return dashapi.InstanceTestResult{}, dashapi.ErrInstancesDisabled
}
func (noSSHInstances) TestProfile(config.SSHProfileConfig) (dashapi.InstanceTestResult, error) {
	return dashapi.InstanceTestResult{}, dashapi.ErrInstancesDisabled
}
func (noSSHInstances) Close() {}
