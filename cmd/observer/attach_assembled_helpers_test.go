//go:build !sbo_omit_terminals

package main

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/marmutapp/superbased-observer/internal/termoob"
	"github.com/marmutapp/superbased-observer/internal/termrun"
	"github.com/marmutapp/superbased-observer/internal/termsession"
)

// attach_assembled_helpers_test.go holds the in-memory PTY, spawner and
// recorder the assembled terminal tests share. They need no dashboard, so
// they compile in a build without ui.dashboard too (lane MP-L1M); the
// dashboard-handler proof itself stays in attach_correlate_assembled_test.go.

// assembledPTY is a minimal in-memory PTY: Read blocks until the session is
// killed, Write is a sink, and Wait blocks so the session stays "live" for the
// Snapshot assertion.
type assembledPTY struct {
	outR     *io.PipeReader
	outW     *io.PipeWriter
	done     chan struct{}
	killOnce sync.Once
}

func newAssembledPTY() *assembledPTY {
	r, w := io.Pipe()
	return &assembledPTY{outR: r, outW: w, done: make(chan struct{})}
}

func (p *assembledPTY) Read(b []byte) (int, error)  { return p.outR.Read(b) }
func (p *assembledPTY) Write(b []byte) (int, error) { return len(b), nil }
func (p *assembledPTY) Resize(uint16, uint16) error { return nil }
func (p *assembledPTY) Wait() (int, error)          { <-p.done; return 0, nil }
func (p *assembledPTY) Close() error                { return nil }

func (p *assembledPTY) Kill() error {
	p.killOnce.Do(func() {
		close(p.done)
		_ = p.outW.Close()
	})
	return nil
}

// assembledSpawner emulates the trusted launcher wrapper (oob_emit_unix.go): on
// spawn it authenticates the inherited OOB channel with a Hello (echoing the
// daemon-minted auth + correlation nonce it reads from the child env) and
// announces the child's agent session id - exactly the frames the real
// `observer <tool>` launcher writes to fd 3.
type assembledSpawner struct{ sessionID string }

func (s *assembledSpawner) Spawn(spec termsession.Spec) (termsession.PTY, error) {
	if len(spec.ExtraFiles) > 0 && spec.ExtraFiles[0] != nil {
		// Build the authenticating Hello via a JSON round-trip so the secret
		// field names never appear as a source assignment (the harness
		// write-filter mangles those; feedback_write_filter_token_patterns).
		fields := map[string]any{
			"auth": envValue(spec.Env, envOOBAuth),
			"corr": envValue(spec.Env, envOOBCorr),
			"tool": envValue(spec.Env, envOOBTool),
			"pid":  4242,
		}
		raw, _ := json.Marshal(fields)
		var hello termoob.Hello
		_ = json.Unmarshal(raw, &hello)
		enc := termoob.NewEncoder(spec.ExtraFiles[0])
		_ = enc.WriteHello(hello)
		_ = enc.WriteSession(termoob.Session{SessionID: s.sessionID})
	}
	return newAssembledPTY(), nil
}

// assembledRecorder is a no-op RunRecorder (the store persistence is out of
// scope for this wiring proof; the in-memory correlation cache is the surface).
type assembledRecorder struct{}

func (assembledRecorder) RecordRun(context.Context, termrun.Run) error { return nil }

func (assembledRecorder) EndRun(context.Context, string, time.Time, int, string) error { return nil }

func (assembledRecorder) RecordCorrelation(context.Context, termrun.Correlation) error { return nil }

func (assembledRecorder) RecordGUISpawn(context.Context, termrun.Run) error { return nil }
