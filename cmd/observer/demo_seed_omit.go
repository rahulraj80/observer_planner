//go:build sbo_omit_demo

package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/marmutapp/superbased-observer/internal/components"
)

// demo_seed_omit.go stands in for demo_seed.go in a build without the demo
// component (-tags sbo_omit_demo, lane MP-L1E, T-715): internal/demo and its
// embedded fixtures are not linked. The /api/demo route belongs to an
// omitted component, so the route gate answers 404 component_not_installed
// before this seeder could run; it refuses anyway for a harness that skips
// the gate.
func init() { components.MarkOmitted("demo") }

// errDemoNotInBuild is the refusal a compiled-out demo seeder returns.
var errDemoNotInBuild = errors.New("demo data (demo) is not in this build")

// demoSeeder returns a seeder that always refuses.
func demoSeeder(*slog.Logger) func(ctx context.Context) (*sql.DB, func() error, error) {
	return func(context.Context) (*sql.DB, func() error, error) {
		return nil, nil, errDemoNotInBuild
	}
}
