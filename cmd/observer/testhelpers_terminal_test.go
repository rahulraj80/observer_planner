//go:build !sbo_omit_terminals

package main

import (
	"context"
	"io"
	"time"

	"github.com/marmutapp/superbased-observer/internal/termrun"
	"github.com/marmutapp/superbased-observer/internal/termsession"
	"github.com/marmutapp/superbased-observer/internal/termsvc"
)

// testhelpers_terminal_test.go holds the process-free terminal fakes several
// test files share (lane MP-L1K, T-715). They used to live in the
// remote-tagged remote_execute_matrix_test.go, which forced unrelated
// terminal tests to carry !sbo_omit_remote.

// --- fakes for a real, process-free termsession/termsvc assembly ---

type fakePTY struct{ done chan struct{} }

func newFakePTY() *fakePTY { return &fakePTY{done: make(chan struct{})} }

func (p *fakePTY) Read([]byte) (int, error)    { <-p.done; return 0, io.EOF }
func (p *fakePTY) Write(b []byte) (int, error) { return len(b), nil }
func (p *fakePTY) Resize(uint16, uint16) error { return nil }
func (p *fakePTY) Wait() (int, error)          { <-p.done; return 0, nil }
func (p *fakePTY) Kill() error {
	select {
	case <-p.done:
	default:
		close(p.done)
	}
	return nil
}
func (p *fakePTY) Close() error { return nil }

type fakeSpawner struct{}

func (fakeSpawner) Spawn(termsession.Spec) (termsession.PTY, error) { return newFakePTY(), nil }

type nopRunRecorder struct{}

func (nopRunRecorder) RecordRun(context.Context, termrun.Run) error                 { return nil }
func (nopRunRecorder) EndRun(context.Context, string, time.Time, int, string) error { return nil }
func (nopRunRecorder) RecordCorrelation(context.Context, termrun.Correlation) error { return nil }
func (nopRunRecorder) RecordGUISpawn(context.Context, termrun.Run) error            { return nil }

type mgrLauncher struct{ mgr *termsession.Manager }

func (l mgrLauncher) Spawn(req termsvc.LaunchRequest) (string, error) {
	return l.mgr.Create(termsession.Spec{
		BinPath:    "/observer",
		Subcommand: req.Subcommand,
		SessionID:  req.SessionID,
		ArgvMode:   argvModeForKind(req.Kind),
	})
}
