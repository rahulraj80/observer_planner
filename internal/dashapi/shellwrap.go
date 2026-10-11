//go:build !sbo_omit_integration_shell_wrap

package dashapi

import (
	"context"

	"github.com/marmutapp/superbased-observer/internal/shellwrap"
	"github.com/marmutapp/superbased-observer/internal/shellwrapsvc"
)

// ShellWrapService is the dashboard's seam onto the command-wrapping shell
// integration (backlog item 7): cmd wires an *shellwrapsvc.Service, the ONE
// writer of the shim directory and the shell start-up-file blocks. Nil = not
// wired (the routes answer 501). Every call names its target: the daemon's own
// shells, or (from inside WSL) the Windows shells of the same machine (T-419).
type ShellWrapService interface {
	StatusFor(ctx context.Context, target shellwrapsvc.Target) (shellwrap.Status, error)
	Apply(ctx context.Context, req shellwrapsvc.Request) (shellwrapsvc.Outcome, error)
	DisableFor(ctx context.Context, target shellwrapsvc.Target, dryRun bool) (shellwrapsvc.Outcome, error)
	Targets(ctx context.Context) ([]shellwrapsvc.TargetInfo, error)
}
