package componentsvc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/marmutapp/superbased-observer/internal/components"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// PurgeStore is the store seam of the component purge
// (internal/store/componentpurge.go). *store.Store implements it.
type PurgeStore interface {
	CreateComponentPurgePlan(ctx context.Context, spec store.ComponentPurgeSpec) (store.ComponentPurgePlan, error)
	ExecuteComponentPurgePlan(ctx context.Context, id string, recheck store.PurgeRecheck, now time.Time) (store.PurgeResult, error)
	ComponentPurgePlanByID(ctx context.Context, id string) (store.ComponentPurgePlan, error)
}

// ErrPurgeUnavailable means no store is wired in this process.
var ErrPurgeUnavailable = errors.New("componentsvc: purge is not available in this process (no database)")

// PurgePreview builds and persists an interactive purge plan for an
// INACTIVE component: the exact candidate rows, their counts per table, and
// an opaque plan id the confirmation must name within 15 minutes. An active
// component is refused (remove it first; data is kept on removal).
func (s *Service) PurgePreview(ctx context.Context, component string) (store.ComponentPurgePlan, error) {
	if s.opts.Purge == nil {
		return store.ComponentPurgePlan{}, ErrPurgeUnavailable
	}
	if err := s.refuseActive(ctx, component); err != nil {
		return store.ComponentPurgePlan{}, err
	}
	return s.opts.Purge.CreateComponentPurgePlan(ctx, store.ComponentPurgeSpec{
		Kind: store.PurgeInteractive, Component: component, CatalogVersion: components.CatalogVersion,
	})
}

// PurgeExecute consumes an interactive plan by id. Inside the store's one
// transaction the component is re-checked against the CURRENT selection,
// so a component re-activated since the preview is refused. The whole
// execution holds the config lock Apply takes (ledger MODPKG-12), so an
// Apply in this or another process cannot re-activate the component
// between that re-check and the commit: it waits for the purge, or gives
// up with the lock's timeout. Audited.
func (s *Service) PurgeExecute(ctx context.Context, planID string) (store.PurgeResult, error) {
	if s.opts.Purge == nil {
		return store.PurgeResult{}, ErrPurgeUnavailable
	}
	release, err := s.lockConfig()
	if err != nil {
		return store.PurgeResult{}, err
	}
	defer release()
	recheck := func(ctx context.Context, plan store.ComponentPurgePlan) error {
		return s.refuseActive(ctx, plan.Component)
	}
	res, err := s.opts.Purge.ExecuteComponentPurgePlan(ctx, planID, recheck, time.Time{})
	if err != nil {
		return res, err
	}
	if s.opts.Audit != nil {
		s.opts.Audit(ctx, "components.purge", fmt.Sprintf("plan=%s deleted=%v skipped=%v kept_sessions=%d", planID, res.Deleted, res.Skipped, res.KeptSessions))
	}
	return res, nil
}

func (s *Service) refuseActive(ctx context.Context, component string) error {
	c, ok := components.Get(component)
	if !ok || c.Binary != components.BinaryNode {
		return fmt.Errorf("componentsvc: %q is not a node component", component)
	}
	if !store.HasPurgePlan(component) {
		return fmt.Errorf("componentsvc: %s: %w", component, store.ErrNoPurgePlan)
	}
	snap, err := s.State(ctx)
	if err != nil {
		return err
	}
	if snap.Active(component) {
		return fmt.Errorf("componentsvc: %s is active; remove it first (its data is kept on removal, and only then can it be purged)", component)
	}
	return nil
}
