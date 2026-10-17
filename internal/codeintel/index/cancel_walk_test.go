//go:build !sbo_omit_codeintel

package index_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"

	"github.com/marmutapp/superbased-observer/internal/codeintel/index"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestIndexProjectStopsTheWalkOnCancel pins T-553 for codeintel: once the
// daemon is stopping, an index pass must not walk the project tree. The
// candidate walk used to take no context, so a large root (or one on the
// slow WSL DrvFs mount) was walked to the end after SIGTERM and only the
// per-file step noticed the stop. A cancelled pass now scans nothing and
// reports the cancellation.
func TestIndexProjectStopsTheWalkOnCancel(t *testing.T) {
	database, err := dbtemplate.Open(context.Background(), db.Options{Path: filepath.Join(t.TempDir(), "obs.db")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()
	st := store.New(database)

	root := t.TempDir()
	for i := 0; i < 20; i++ {
		writeFile(t, filepath.Join(root, fmt.Sprintf("p%02d", i), "f.go"), fmt.Sprintf("package p%02d\nfunc F() {}\n", i))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ix := index.New(index.Options{Store: st, Registry: index.DefaultRegistry()})
	rep, err := ix.IndexProject(ctx, root, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("IndexProject on a cancelled context: err = %v, want context.Canceled", err)
	}
	if rep.Scanned != 0 || rep.Indexed != 0 {
		t.Fatalf("a cancelled pass still walked the tree: scanned=%d indexed=%d", rep.Scanned, rep.Indexed)
	}
}
