//go:build !sbo_omit_terminals_sandbox && !sbo_omit_terminals

package main

import "github.com/marmutapp/superbased-observer/internal/sandbox"

// sandboxEnvMarker is the env var a sandboxed launch carries (set on the
// child inside the boundary, stripped from inherited env). Its owner is
// internal/sandbox; terminal_sandbox_omit.go repeats the literal for a build
// without terminals.sandbox, which still strips a spoofed marker.
const sandboxEnvMarker = sandbox.EnvMarker
