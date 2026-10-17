//go:build !sbo_omit_adapter_grokbot

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/grokbot"
)

// adapter.grokbot (package internal/adapter/grokbot). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_grokbot_omit.go reports the component compiled out (L1).
func init() {
	register(37, func() adapter.Adapter { return grokbot.New() })
}
