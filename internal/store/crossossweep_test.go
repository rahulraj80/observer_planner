package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/procmodel"
)

// sweepFixtureStore opens a migrated store for a test or a benchmark.
func sweepFixtureStore(tb testing.TB) *Store {
	tb.Helper()
	database, err := dbtemplate.Open(context.Background(), db.Options{Path: filepath.Join(tb.TempDir(), "sweep.db")})
	if err != nil {
		tb.Fatalf("db.Open: %v", err)
	}
	tb.Cleanup(func() { database.Close() })
	return New(database)
}

// seedSweepSession inserts a claude-code session in C:\proj that started at
// start, and returns its id.
func seedSweepSession(tb testing.TB, s *Store, id string, start time.Time) {
	tb.Helper()
	ctx := context.Background()
	projID, err := s.UpsertProject(ctx, `C:\proj`, "")
	if err != nil {
		tb.Fatalf("UpsertProject: %v", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, project_id, tool, started_at) VALUES (?, ?, ?, ?)`,
		id, projID, "claude-code", timestamp(start)); err != nil {
		tb.Fatalf("seed session: %v", err)
	}
}

// TestSweepCrossOSSkipsConvergedSession pins T-413's incremental sweep: once a
// session's pass attributes nothing, the next sweep skips it unless a newly
// captured row can enter its pool; unrelated process churn does not wake it,
// a late child of its tree does (and is attributed), and the full pass still
// runs at least every crossOSFullEvery.
func TestSweepCrossOSSkipsConvergedSession(t *testing.T) {
	t.Parallel()
	s := sweepFixtureStore(t)
	ctx := context.Background()
	start := time.Now().UTC().Add(-3 * time.Hour)
	seedSweepSession(t, s, "sw", start)
	root := unattributedRun("sw_root", "", "claude.exe", `C:\proj`, 5001, start.Add(2*time.Second))
	child := unattributedRun("sw_child", "sw_root", "node.exe", `C:\proj`, 5002, start.Add(time.Minute))
	if _, err := s.PersistRuns(ctx, []procmodel.ProcessRun{root, child}); err != nil {
		t.Fatal(err)
	}

	sw := NewCrossOSSweeper()
	clock := start.Add(3 * time.Hour)
	sw.now = func() time.Time { return clock }
	sweep := func(label string) CrossOSSweepResult {
		t.Helper()
		res, err := s.SweepCrossOS(ctx, sw, []string{"sw"})
		if err != nil {
			t.Fatalf("%s: SweepCrossOS: %v", label, err)
		}
		return res
	}

	if r := sweep("first"); r.FullPasses != 1 || r.Attributed != 2 {
		t.Fatalf("first sweep = %+v, want one full pass attributing 2", r)
	}
	// A pass that attributed rows is run again: it can widen the next pool.
	if r := sweep("converge"); r.FullPasses != 1 || r.Attributed != 0 {
		t.Fatalf("converging sweep = %+v, want one full pass attributing 0", r)
	}
	// Nothing new: skipped.
	if r := sweep("idle"); r.Skipped != 1 || r.FullPasses != 0 {
		t.Fatalf("idle sweep = %+v, want skipped", r)
	}
	// Unrelated churn (another tree, outside the window) does not wake it.
	noise := unattributedRun("sw_noise", "explorer", "svchost.exe", `C:\Windows`, 5003, start.Add(2*time.Hour))
	if _, err := s.PersistRuns(ctx, []procmodel.ProcessRun{noise}); err != nil {
		t.Fatal(err)
	}
	if r := sweep("churn"); r.Skipped != 1 || r.FullPasses != 0 {
		t.Fatalf("churn sweep = %+v, want skipped", r)
	}
	// A late child of the session's tree wakes it and is attributed.
	late := unattributedRun("sw_late", "sw_child", "git.exe", `C:\proj`, 5004, start.Add(150*time.Minute))
	if _, err := s.PersistRuns(ctx, []procmodel.ProcessRun{late}); err != nil {
		t.Fatal(err)
	}
	if r := sweep("late child"); r.FullPasses != 1 || r.Attributed != 1 {
		t.Fatalf("late-child sweep = %+v, want one full pass attributing 1", r)
	}
	runs, err := s.ProcessRunsForSession(ctx, "sw")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3 {
		t.Fatalf("session has %d runs, want 3 (root, child, late child; noise excluded)", len(runs))
	}
	if r := sweep("converge again"); r.FullPasses != 1 {
		t.Fatalf("post-attribution sweep = %+v, want a full pass", r)
	}
	if r := sweep("idle again"); r.Skipped != 1 {
		t.Fatalf("idle sweep = %+v, want skipped", r)
	}
	// The full-pass bound: an in-place change carries no new id, so the pass
	// still runs once the bound elapses.
	clock = clock.Add(crossOSFullEvery + time.Second)
	if r := sweep("bound"); r.FullPasses != 1 {
		t.Fatalf("sweep after the full-pass bound = %+v, want a full pass", r)
	}
}

// TestSweepCrossOSNilSweeperRunsEveryPass pins the compatibility path: no
// sweeper means a full pass per session, the pre-T-413 behaviour the lazy
// triggers and the old sweep entry point rely on.
func TestSweepCrossOSNilSweeperRunsEveryPass(t *testing.T) {
	t.Parallel()
	s := sweepFixtureStore(t)
	ctx := context.Background()
	start := time.Now().UTC().Add(-time.Hour)
	seedSweepSession(t, s, "nil", start)
	for i := 0; i < 3; i++ {
		res, err := s.SweepCrossOS(ctx, nil, []string{"nil", "missing"})
		if err != nil {
			t.Fatal(err)
		}
		if res.FullPasses != 2 || res.Skipped != 0 {
			t.Fatalf("pass %d = %+v, want 2 full passes and no skip", i, res)
		}
	}
}

// BenchmarkSweepCrossOSConverged measures one background sweep tick over a
// converged long-lived session (a 20,000-process tree) with 200 rows of
// unrelated churn per tick: every=full is the pre-T-413 sweep, incremental
// is the sweeper.
func BenchmarkSweepCrossOSConverged(b *testing.B) {
	for _, mode := range []string{"every=full", "incremental"} {
		b.Run(mode, func(b *testing.B) {
			s := sweepFixtureStore(b)
			ctx := context.Background()
			start := time.Now().UTC().Add(-9 * 24 * time.Hour)
			seedSweepSession(b, s, "big", start)
			runs := []procmodel.ProcessRun{unattributedRun("big_root", "", "claude.exe", `C:\proj`, 1, start.Add(time.Second))}
			for i := 0; i < 20000; i++ {
				parent := "big_root"
				if i > 0 && i%10 != 0 {
					parent = fmt.Sprintf("big_%d", i-1)
				}
				runs = append(runs, unattributedRun(fmt.Sprintf("big_%d", i), parent, "node.exe", `C:\proj`, 10+i, start.Add(time.Duration(i)*time.Second)))
			}
			if _, err := s.PersistRuns(ctx, runs); err != nil {
				b.Fatal(err)
			}
			var sw *CrossOSSweeper
			if mode == "incremental" {
				sw = NewCrossOSSweeper()
			}
			for i := 0; i < 3; i++ { // converge
				if _, err := s.SweepCrossOS(ctx, sw, []string{"big"}); err != nil {
					b.Fatal(err)
				}
			}
			pid := 100000
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				churn := make([]procmodel.ProcessRun, 0, 200)
				for j := 0; j < 200; j++ {
					pid++
					churn = append(churn, unattributedRun(fmt.Sprintf("churn_%d", pid), "", "svchost.exe", `C:\Windows`, pid, time.Now().UTC()))
				}
				if _, err := s.PersistRuns(ctx, churn); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				if _, err := s.SweepCrossOS(ctx, sw, []string{"big"}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
