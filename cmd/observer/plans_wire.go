package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/keystore"
	"github.com/marmutapp/superbased-observer/internal/planlimitslocal"
	"github.com/marmutapp/superbased-observer/internal/planlimitsnet"
	"github.com/marmutapp/superbased-observer/internal/planlimitssvc"
	"github.com/marmutapp/superbased-observer/internal/shimguard"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// Plans & limits wiring (docs/plans-and-limits.md). This file is the one
// place the module's I/O packages are composed: the egress
// (internal/planlimitsnet), the vendor-local reader (internal/planlimitslocal)
// and the keychain (internal/keystore) are handed to the service, which owns
// no I/O of its own. tests/invariant/planlimits_egress_test.go pins that only
// the service and this command import the I/O packages.

// plansCredDir is the hardened file fallback for the keychain, next to the
// database. It is used only when the OS keychain is unusable and the file
// store's owner and permission checks pass; otherwise saving a key fails.
func plansCredDir(cfg config.Config) string {
	return filepath.Join(filepath.Dir(cfg.Observer.DBPath), "plans-cred")
}

// lazyKeys opens the keychain on first use, so a daemon with no account
// never probes the OS keychain at all.
type lazyKeys struct {
	once   sync.Once
	dir    string
	logger *slog.Logger
	s      keystore.Store
	opened atomic.Bool
	// open overrides keystore.Open (tests).
	open func() keystore.Store
}

func (k *lazyKeys) store() keystore.Store {
	k.once.Do(func() {
		if k.open != nil {
			k.s = k.open()
		} else {
			k.s = keystore.Open(planlimitssvc.KeystoreService, k.dir, k.logger)
		}
		k.opened.Store(true)
	})
	return k.s
}

// Get, Set and Delete open the keychain on first use.
func (k *lazyKeys) Get(name string) (string, error) { return k.store().Get(name) }

// Set stores a secret.
func (k *lazyKeys) Set(name, value string) error { return k.store().Set(name, value) }

// Delete removes a secret.
func (k *lazyKeys) Delete(name string) error { return k.store().Delete(name) }

// Backend names the backend without opening it: the page model asks on
// every load, and a page view must not probe the keychain.
func (k *lazyKeys) Backend() string {
	if !k.opened.Load() {
		return "not opened"
	}
	return k.s.Backend()
}

func (k *lazyKeys) SecurityDiagnostic() string {
	return k.store().SecurityDiagnostic()
}

// newPlansService composes the service over database.
func newPlansService(cfg config.Config, database *sql.DB, logger *slog.Logger) *planlimitssvc.Service {
	home, _ := os.UserHomeDir()
	var exclude []string
	if home != "" {
		exclude = append(exclude, shimguard.DefaultShimDir(runtime.GOOS, home))
	}
	return planlimitssvc.New(planlimitssvc.Options{
		Store:  store.New(database),
		Keys:   &lazyKeys{dir: plansCredDir(cfg), logger: logger},
		Net:    planlimitsnet.New(planlimitsnet.Options{Version: version, ExcludeDirs: exclude}),
		Local:  planlimitslocal.Reader{},
		Logger: logger,
	})
}

// processPlans is the daemon's one service: the dashboard API and the poll
// loop share it, so the page-open cadence and the status line dedup have
// one owner. The first caller's database handle wins; every handle reaches
// the same file.
var (
	processPlansMu  sync.Mutex
	processPlansSvc *planlimitssvc.Service
)

func processPlans(cfg config.Config, database *sql.DB, logger *slog.Logger) *planlimitssvc.Service {
	processPlansMu.Lock()
	defer processPlansMu.Unlock()
	if processPlansSvc == nil {
		processPlansSvc = newPlansService(cfg, database, logger)
	}
	return processPlansSvc
}

// plansLoop runs the poll loop until ctx ends. With no account added it
// reads the local database only (zero egress). Fail-soft: a config or
// database error idles the loop, never the daemon.
func plansLoop(ctx context.Context, configPath string) {
	cfg, database, cleanup, err := loadConfigAndDB(ctx, configPath)
	if err != nil {
		return
	}
	defer cleanup()
	processPlans(cfg, database, newLogger(cfg.Observer.LogLevel)).Run(ctx)
}
