package shellwrapsvc

import (
	"github.com/marmutapp/superbased-observer/internal/shimguard"
)

// LookPath (owner: internal/shimguard, lane MP-L1E) is exec.LookPath for callers that want the VENDOR binary: a hit
// that is a command-wrapping shim (it carries shellwrap.ShimMarker) is
// skipped and the PATH walk continues, so a shim named `claude` is never
// reported as "claude is installed" nor run by a caller that sets up its own
// routing. Launchers resolve through internal/toolresolve, which applies the
// same exclusion; this is for the few call sites that use a bare PATH lookup.
func LookPath(name string) (string, error) { return shimguard.LookPath(name) }
