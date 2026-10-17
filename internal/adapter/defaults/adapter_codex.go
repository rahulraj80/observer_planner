//go:build !sbo_omit_adapter_codex && !sbo_omit_adapter_open_interpreter

package defaults

import (
	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/codex"
)

// adapter.codex and adapter.open-interpreter (package internal/adapter/codex). A build with any tag in the
// constraint above leaves the package out of the binary, and
// adapter_codex_omit.go reports the component compiled out (L1).
func init() {
	register(2, func() adapter.Adapter { return codex.New() })
	// Open Interpreter is a rebadged Codex CLI Rust build: the codex
	// parser re-tags every event at the single boundary seam (the §2.1
	// variant-adapter pattern), so it ships with the codex package.
	register(29, func() adapter.Adapter { return codex.NewOpenInterpreter() })
}
