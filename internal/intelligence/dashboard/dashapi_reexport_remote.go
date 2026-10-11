//go:build !sbo_omit_remote

package dashboard

import "github.com/marmutapp/superbased-observer/internal/dashapi"

// RemoteOptions re-exports dashapi.RemoteOptions (a remote build only, like
// the type itself).
type RemoteOptions = dashapi.RemoteOptions
