//go:build sbo_omit_terminals_ssh

package termsvc

import (
	"errors"

	"github.com/marmutapp/superbased-observer/internal/sshspec"
)

// errSSHNotInBuild is what an SSH launch returns in a build without the
// terminals.ssh component: the profile was found, but nothing in this binary
// composes an ssh argv, so the launch is refused before a run is minted.
var errSSHNotInBuild = errors.New("termsvc: terminals.ssh is not in this build")

func sshLaunchArgv(sshspec.Profile, sshspec.Options) ([]string, error) {
	return nil, errSSHNotInBuild
}
