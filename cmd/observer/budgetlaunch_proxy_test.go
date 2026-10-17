//go:build !sbo_omit_capture_proxy

package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/policy"
	"github.com/marmutapp/superbased-observer/internal/proxy"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// budgetlaunch_proxy_test.go holds the budget test that wires the guard's
// proxy seam (moved from budgetlaunch_test.go, lane MP-L1L, T-715).

func TestWireGuardProxyKeepsBudgetScannerWhenOtherScansOff(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "observer.db")
	database, err := dbtemplate.Open(ctx, db.Options{Path: dbPath})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer func() { _ = database.Close() }()

	processGuards.mu.Lock()
	delete(processGuards.m, dbPath)
	processGuards.mu.Unlock()
	t.Cleanup(func() {
		processGuards.mu.Lock()
		delete(processGuards.m, dbPath)
		processGuards.mu.Unlock()
	})

	cfg := config.Config{
		Observer: config.ObserverConfig{DBPath: dbPath},
		Guard:    config.GuardConfig{Enabled: true, Mode: "enforce"},
	}
	var opts proxy.Options
	wireGuardProxy(ctx, cfg, store.New(database), &opts,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if opts.Guard == nil {
		t.Fatal("proxy Guard is nil with all proxy/MCP scan toggles off; budget scanning would be absent")
	}
	if lookupProcessGuard(dbPath) == nil {
		t.Fatal("wireGuardProxy did not publish the shared process Guard")
	}
	if err := lookupProcessGuard(dbPath).ApplyOrgBudget(config.GuardBudgetConfig{}, nil, true, policy.BudgetProtection{}, ""); err != nil {
		t.Fatalf("ApplyOrgBudget(required): %v", err)
	}
	got := opts.Guard.ScanRequest(ctx, "openai", []byte(`{"model":"test"}`), "")
	if got.Action != "deny" || got.RuleID != "B-625" {
		t.Fatalf("required-budget scan = %+v, want deny B-625", got)
	}
}
