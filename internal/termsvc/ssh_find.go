package termsvc

import "github.com/marmutapp/superbased-observer/internal/sshspec"

// findSSHProfile looks a profile up by exact name, the same rule as
// sshprofile.Find: an unknown name yields ok=false and the launch fails, so a
// name that is not in the operator's config never reaches an argv.
func findSSHProfile(profiles []sshspec.Profile, name string) (sshspec.Profile, bool) {
	for _, p := range profiles {
		if p.Name == name {
			return p, true
		}
	}
	return sshspec.Profile{}, false
}
