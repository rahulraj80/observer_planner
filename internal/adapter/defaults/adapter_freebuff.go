//go:build !sbo_omit_adapter_freebuff

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/freebuff"
)

// adapter.freebuff (package internal/adapter/freebuff). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_freebuff_omit.go reports the component compiled out (L1).
func init() {
	register(36, func() adapter.Adapter { return freebuff.New() })
}
