//go:build sbo_omit_adapter_antigravity || sbo_omit_adapter_antigravity_cli

package main

import (
	"context"
	"database/sql"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// errAntigravityNotInBuild is what --antigravity-project-root answers in a
// build that compiled adapter.antigravity out (lane MP-L1D, T-598).
var errAntigravityNotInBuild = adapterPassNotInBuild("--antigravity-project-root", models.ToolAntigravity)

// antigravityProjectRootBackfillDirs has no roots without the adapter.
func antigravityProjectRootBackfillDirs() []string { return nil }

// backfillAntigravityProjectRoot refuses: the adapter is compiled out.
func backfillAntigravityProjectRoot(context.Context, *sql.DB, []string, int) (AntigravityProjectRootBackfill, error) {
	return AntigravityProjectRootBackfill{}, errAntigravityNotInBuild
}
