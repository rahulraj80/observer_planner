//go:build !sbo_omit_adapter_gemini_cli

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/gemini"
)

// adapter.gemini-cli (package internal/adapter/gemini). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_gemini_omit.go reports the component compiled out (L1).
func init() {
	register(13, func() adapter.Adapter { return gemini.New() })
}
