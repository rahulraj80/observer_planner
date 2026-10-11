//go:build !sbo_omit_adapter_goose

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/goose"
)

// adapter.goose (package internal/adapter/goose). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_goose_omit.go reports the component compiled out (L1).
func init() {
	register(27, func() adapter.Adapter { return goose.New() })
}
