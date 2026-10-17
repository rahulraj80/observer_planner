//go:build sbo_omit_terminals_sandbox && !sbo_omit_terminals

package main

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/dashapi"
	"github.com/marmutapp/superbased-observer/internal/termsvc"
)

// terminal_sandbox_omit.go stands in for terminal_sandbox.go,
// terminal_sandbox_marker.go and the sandbox_helpers*.go dispatch in a build
// without the terminals.sandbox component (-tags sbo_omit_terminals_sandbox,
// lane MP-L1E, T-715): internal/sandbox and internal/sandboxnet are not
// linked. The terminal stack still gets a holder, so its wiring is
// unchanged, but the holder never has a runtime: Prepare fails closed with
// termsvc.ErrSandboxUnavailable (exactly what a nil Sandboxer returns, so a
// sandbox-required launch is refused, never run unsandboxed) and the probe
// says the sandbox is not in this build.
func init() { components.MarkOmitted("terminals.sandbox") }

// sandboxEnvMarker repeats internal/sandbox.EnvMarker: a build without the
// sandbox never sets it, but still strips a spoofed one from a child's env.
const sandboxEnvMarker = "OBSERVER_SANDBOX"

// sandboxHolder has no runtime in this build.
type sandboxHolder struct {
	managedRoot string
}

// newSandboxHolder builds the runtime-less holder. The managed-workspaces
// root is resolved exactly as the full build does, so `observer workspaces`
// and termsvc agree on it.
func newSandboxHolder(cfg config.Config, _ string, observerDir, _ string, _ *slog.Logger) *sandboxHolder {
	h := &sandboxHolder{managedRoot: defaultWorkspacesDir(cfg.Terminal.Sandbox, observerDir)}
	if resolved, err := filepath.EvalSymlinks(h.managedRoot); err == nil {
		h.managedRoot = resolved
	}
	return h
}

// workspacesDir returns the managed-workspaces root.
func (h *sandboxHolder) workspacesDir() string { return h.managedRoot }

// ProbeSandbox reports the sandbox unavailable, naming the build.
func (h *sandboxHolder) ProbeSandbox(context.Context) dashapi.SandboxAvailability {
	return dashapi.SandboxAvailability{
		Available: false,
		Verdict:   dashapi.VerdictNotInBuild,
		Reason:    "the terminal sandbox (terminals.sandbox) is not in this build; " + components.DistrosHaving("terminals.sandbox") + ". Install a build that includes it.",
	}
}

// Prepare fails closed: no sandboxed launch can run in this build.
func (h *sandboxHolder) Prepare(context.Context, termsvc.PrepareRequest) (termsvc.PrepareResult, error) {
	return termsvc.PrepareResult{}, termsvc.ErrSandboxUnavailable
}

// sandboxHelperDispatch runs no helper: a build without the sandbox never
// plans a sandboxed launch, so its helper verbs never reach this binary.
func sandboxHelperDispatch([]string) (int, bool) { return 0, false }
