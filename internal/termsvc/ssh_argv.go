//go:build !sbo_omit_terminals_ssh

package termsvc

import (
	"github.com/marmutapp/superbased-observer/internal/sshprofile"
	"github.com/marmutapp/superbased-observer/internal/sshspec"
)

// sshLaunchArgv composes the interactive OpenSSH argv for a profile. It is
// the one place termsvc reaches the terminals.ssh package; a build with
// sbo_omit_terminals_ssh pairs it with ssh_argv_omit.go (lane MP-L1H, T-715).
func sshLaunchArgv(p sshspec.Profile, o sshspec.Options) ([]string, error) {
	return sshprofile.Argv(p, o)
}
