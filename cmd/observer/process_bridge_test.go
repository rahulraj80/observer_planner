//go:build !sbo_omit_processobs

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/processobs"
	"github.com/marmutapp/superbased-observer/internal/processobs/bridge"
)

// bridgeFirstEventDeadline bounds how long a capturer test waits for the
// initial snapshot's first event frame. It is a CEILING, not a budget: the
// helper returns the moment the first event lands (milliseconds on an idle
// host). It is generous because the snapshot walks every /proc entry (or the
// ToolHelp table) and reads each process's environ for the session token, so
// under -race on a loaded 2-vCPU VM it can take seconds, and a fixed short
// context (the old 400 ms) failed there with "expected at least one process
// event" (T-310). The production capturer has no such budget at all: it runs
// for the command's lifetime and the snapshot simply arrives later.
const bridgeFirstEventDeadline = 2 * time.Minute

// eventWatchWriter is the capturer's io.Writer in tests. It keeps the whole
// stream and closes sawEvent the first time a complete event frame is
// written, so a test can wait on the CONDITION (an event was emitted) rather
// than on a wall-clock guess.
type eventWatchWriter struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	pending  []byte
	seen     bool
	sawEvent chan struct{}
}

func newEventWatchWriter() *eventWatchWriter {
	return &eventWatchWriter{sawEvent: make(chan struct{})}
}

// Write records p and scans the complete lines it finishes for an event frame.
// The encoder may split one frame over several writes (bufio), so lines are
// reassembled before decoding.
func (w *eventWatchWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	if w.seen {
		return len(p), nil
	}
	w.pending = append(w.pending, p...)
	for {
		i := bytes.IndexByte(w.pending, '\n')
		if i < 0 {
			break
		}
		line := w.pending[:i]
		w.pending = w.pending[i+1:]
		f, err := bridge.NewDecoder(bytes.NewReader(line)).Next()
		if err == nil && f.Kind == bridge.KindEvent {
			w.seen = true
			w.pending = nil
			close(w.sawEvent)
			break
		}
	}
	return len(p), nil
}

// stream returns a copy of everything written so far.
func (w *eventWatchWriter) stream() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buf.Bytes()...)
}

// captureUntilFirstEvent runs one capturer (run) until its first process event
// frame is written, then cancels it and returns the whole stream. It fails the
// test only if no event arrives within bridgeFirstEventDeadline, or the
// capturer returns an error. A capturer that returns on its own before
// emitting an event (its backend closed) returns the stream as-is so the
// caller's own "no events" assertion reports it.
func captureUntilFirstEvent(t *testing.T, run func(ctx context.Context, w io.Writer) error) []byte {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := newEventWatchWriter()
	done := make(chan error, 1)
	go func() { done <- run(ctx, w) }()

	timer := time.NewTimer(bridgeFirstEventDeadline)
	defer timer.Stop()
	select {
	case <-w.sawEvent:
	case err := <-done:
		if err != nil {
			t.Fatalf("capturer returned before emitting an event: %v", err)
		}
		return w.stream()
	case <-timer.C:
		cancel()
		<-done
		t.Fatalf("no process event from the initial snapshot within %s", bridgeFirstEventDeadline)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("capturer: %v", err)
	}
	return w.stream()
}

// TestStreamProcessBridgeEmitsHelloAndEvents drives the capturer's stream
// contract against the real (local) process table: a hello frame first, then
// at least one event from the initial snapshot, all valid NDJSON. Cross-OS —
// exercises ToolHelp+PEB on the Windows host and /proc under WSL.
func TestStreamProcessBridgeEmitsHelloAndEvents(t *testing.T) {
	raw := captureUntilFirstEvent(t, func(ctx context.Context, w io.Writer) error {
		return streamProcessBridge(ctx, w, 300*time.Millisecond)
	})

	dec := bridge.NewDecoder(bytes.NewReader(raw))
	first, err := dec.Next()
	if err != nil {
		t.Fatalf("decode hello: %v", err)
	}
	if first.Kind != bridge.KindHello {
		t.Fatalf("first frame kind = %q, want hello", first.Kind)
	}
	if first.V != bridge.WireVersion {
		t.Fatalf("hello wire version = %d, want %d", first.V, bridge.WireVersion)
	}
	if first.Hello == nil || first.Hello.OS == "" || first.Hello.Backend != "poll" {
		t.Fatalf("hello frame malformed: %+v", first.Hello)
	}

	events := 0
	for {
		f, err := dec.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if f.Kind == bridge.KindEvent {
			if f.Event == nil {
				t.Fatal("event frame has nil Event")
			}
			events++
		}
	}
	if events == 0 {
		t.Fatal("expected at least one process event from the initial snapshot")
	}
}

// TestEventWatchWriterWaitsForTheCondition pins the helper itself: it must
// not signal on the hello/stats/error frames that precede the snapshot, must
// signal on the first event even when that frame arrives split across writes,
// and must keep every byte for the decoder.
func TestEventWatchWriterWaitsForTheCondition(t *testing.T) {
	var frames bytes.Buffer
	enc := bridge.NewEncoder(&frames)
	if err := enc.Hello(bridge.Hello{Backend: "poll", OS: "linux", PID: 1}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Errorf("etw: unavailable"); err != nil {
		t.Fatal(err)
	}
	preamble := append([]byte(nil), frames.Bytes()...)

	w := newEventWatchWriter()
	if _, err := w.Write(preamble); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.sawEvent:
		t.Fatal("signalled on a hello/error frame before any event")
	default:
	}

	frames.Reset()
	if err := enc.Event(processobs.RawEvent{Type: processobs.EventExec, PID: 42, ExePath: "/usr/bin/true"}); err != nil {
		t.Fatal(err)
	}
	ev := frames.Bytes()
	half := len(ev) / 2
	if _, err := w.Write(ev[:half]); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.sawEvent:
		t.Fatal("signalled on half an event frame")
	default:
	}
	if _, err := w.Write(ev[half:]); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.sawEvent:
	default:
		t.Fatal("did not signal once the event frame was complete")
	}
	if got, want := len(w.stream()), len(preamble)+len(ev); got != want {
		t.Fatalf("stream kept %d bytes, want %d", got, want)
	}
}
