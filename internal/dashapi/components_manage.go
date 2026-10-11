package dashapi

import (
	"context"

	"github.com/marmutapp/superbased-observer/internal/componentstate"
	"github.com/marmutapp/superbased-observer/internal/componentsvc"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// ComponentsService is the subset of *componentsvc.Service the dashboard
// uses. cmd/observer wires the production service (newComponentsService);
// tests inject a service over a throwaway config file.
type ComponentsService interface {
	State(ctx context.Context) (componentstate.Snapshot, error)
	Preview(ctx context.Context, ch componentsvc.Change) (componentsvc.Preview, error)
	Apply(ctx context.Context, ch componentsvc.Change, ao componentsvc.ApplyOptions) (componentsvc.ApplyResult, error)
	RunTeardown(ctx context.Context, ids []string, dryRun bool) ([]componentsvc.StepOutcome, error)
	PurgePreview(ctx context.Context, component string) (store.ComponentPurgePlan, error)
	PurgeExecute(ctx context.Context, planID string) (store.PurgeResult, error)
}
