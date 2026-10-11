package dashapi

import (
	"context"

	"github.com/marmutapp/superbased-observer/internal/diag"
)

// WindowsHomeApply is one Windows-home init request.
type WindowsHomeApply struct {
	// Target is the cross-OS target ("claude-code-windows").
	Target string
	// Home is the chosen Windows USER home (e.g. /mnt/c/Users/<you>), one of
	// the detected candidates.
	Home   string
	DryRun bool
	Force  bool
}

// WindowsHomeService is the dashboard's seam onto Windows-home init: cmd
// wires it over the one detection (diag.WindowsHomeTargets) and the one
// writer (`observer init`'s wireAIClients). Nil = not wired (501).
type WindowsHomeService interface {
	// Targets reports whether this daemon runs inside WSL and, if so, the
	// cross-OS targets with their detected Windows homes.
	Targets(ctx context.Context) (wsl bool, targets []diag.WindowsHomeTarget, err error)
	// Apply runs (or, with DryRun, previews) the writes and returns the
	// result lines `observer init` prints.
	Apply(ctx context.Context, req WindowsHomeApply) ([]string, error)
}
