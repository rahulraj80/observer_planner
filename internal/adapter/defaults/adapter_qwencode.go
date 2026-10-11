//go:build !sbo_omit_adapter_qwen_code

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/qwencode"
)

// adapter.qwen-code (package internal/adapter/qwencode). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_qwencode_omit.go reports the component compiled out (L1).
func init() {
	register(19, func() adapter.Adapter { return qwencode.New() })
}
