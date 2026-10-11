package termsvc

import "github.com/marmutapp/superbased-observer/internal/termcore"

// projectroot.go re-exports the project-root and managed-workspace validators
// that moved to core internal/termcore (lane MP-L1K, T-715), so the dashboard
// can validate an allow-list entry without importing the terminals component.

// ErrProjectRootDenied is termcore.ErrProjectRootDenied.
var ErrProjectRootDenied = termcore.ErrProjectRootDenied

// ErrManagedWorkspaceDenied is termcore.ErrManagedWorkspaceDenied.
var ErrManagedWorkspaceDenied = termcore.ErrManagedWorkspaceDenied

// ValidateProjectRoot is termcore.ValidateProjectRoot.
func ValidateProjectRoot(requested string, allowedRoots []string) (string, error) {
	return termcore.ValidateProjectRoot(requested, allowedRoots)
}

// ValidateManagedWorkspace is termcore.ValidateManagedWorkspace.
func ValidateManagedWorkspace(path, managedRoot string) (string, error) {
	return termcore.ValidateManagedWorkspace(path, managedRoot)
}
