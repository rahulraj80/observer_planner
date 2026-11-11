//go:build !sbo_omit_terminals_sandbox && !sbo_omit_isolation && !sbo_omit_terminals

package main

import (
	"fmt"
	"runtime"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/sandboxbackend"
	"github.com/marmutapp/superbased-observer/internal/sandboxseccomp"
	"github.com/marmutapp/superbased-observer/internal/sandboxselect"
	vocab "github.com/marmutapp/superbased-observer/internal/sandboxvocab"
	"github.com/marmutapp/superbased-observer/internal/termsvc"
)

// terminal_sandbox_seccomp.go is the daemon side of the seccomp filter of
// the bwrap and process_lite rows (gap G16, lane N2): the one place a
// launch resolves [terminal.sandbox].seccomp into a profile id. The program
// itself is built where it is loaded (the host helper for bwrap, the exec
// step for process_lite), from that id; sandbox_seccomp_linux.go loads and
// reads it back.

// sandboxSeccompProfile is the profile a launch on row is planned with: ""
// when [terminal.sandbox].seccomp is off (the default, D7) or the row does
// not load Observer's profile (its grounded Iso.Seccomp field, never its
// name). Otherwise it is the one profile, checked to build on this host
// now, so a launch that could not be filtered is refused here and never
// runs unfiltered.
func sandboxSeccompProfile(cfg config.TerminalSandboxConfig, row sandboxbackend.Capability) (string, error) {
	if !cfg.Seccomp || !row.FiltersWithObserverProfile() {
		return "", nil
	}
	return seccompProfileFor(runtime.GOOS, runtime.GOARCH)
}

// seccompProfileFor is sandboxSeccompProfile's check on a given host.
func seccompProfileFor(goos, goarch string) (string, error) {
	if goos != "linux" {
		return "", fmt.Errorf("terminal_sandbox: [terminal.sandbox].seccomp is on, but the seccomp filter is Linux only (this host is %s); the launch is refused, never run unfiltered", goos)
	}
	id := sandboxseccomp.ProfileObserverDefault
	if _, err := sandboxseccomp.Build(id, goarch); err != nil {
		return "", fmt.Errorf("terminal_sandbox: [terminal.sandbox].seccomp is on, but the filter cannot be built on this host: %w; the launch is refused, never run unfiltered", err)
	}
	return string(id), nil
}

// withSeccompRun stamps what the syscall filter gives a created run on its
// isolation evidence (tracker F-002, lane FU4; terminal_run.seccomp,
// migration 232): resolved from the layers' grounded Iso.Seccomp field and
// [terminal.sandbox].seccomp by sandboxselect.SeccompRunState, never from a
// backend name. A run with no layers (no stack recorded) is left unrecorded.
// With the key on and no layer that filters, the state is not_applicable
// and the launch line says so (audit N2-A9), so the run is never silently
// unfiltered.
func (r *sandboxRuntime) withSeccompRun(res termsvc.CreateResult, err error) (termsvc.CreateResult, error) {
	if err != nil || len(res.Isolation.Stack) == 0 {
		return res, err
	}
	res.Isolation.Seccomp = string(sandboxselect.SeccompRunState(r.cfg.Seccomp, seccompLayers(res.Isolation.Stack)))
	return res, nil
}

// seccompLayers looks a run's stack up in the registry; an id the registry
// does not know contributes no layer (it filters nothing Observer knows of).
func seccompLayers(stack []string) []sandboxbackend.Capability {
	out := make([]sandboxbackend.Capability, 0, len(stack))
	for _, id := range stack {
		if row, ok := sandboxbackend.Get(id); ok {
			out = append(out, row)
		}
	}
	return out
}

// sandboxCLISeccompLabel is the CLI launch line's seccomp part: "" for the
// key-off states (the line reads as before), else ", " and the state's
// words.
func sandboxCLISeccompLabel(state string) string {
	l := sandboxselect.SeccompLabel(vocab.SeccompRun(state))
	if l == "" {
		return ""
	}
	return ", " + l
}
