package watcher

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/adapter"
)

// stubChunkedAdapter seeks to fromOffset and streams complete lines through
// adapter.LimitTail, exactly like the Claude Code and Codex parsers, and
// records every window it was asked to read.
type stubChunkedAdapter struct {
	stubFlagAdapter
	calls  int
	limits []int64
}

func (s *stubChunkedAdapter) CursorSemanticsFor(string) adapter.FileCursorSemantics {
	return adapter.FileCursorSemantics{StreamsFromCursor: true, ChunkedTail: true}
}

func (s *stubChunkedAdapter) ParseSessionFile(ctx context.Context, path string, from int64) (adapter.ParseResult, error) {
	s.calls++
	s.limits = append(s.limits, adapter.ParseByteLimit(ctx))
	f, err := os.Open(path)
	if err != nil {
		return adapter.ParseResult{}, err
	}
	defer f.Close()
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return adapter.ParseResult{}, err
	}
	res := adapter.ParseResult{NewOffset: from}
	r := bufio.NewReader(adapter.LimitTail(ctx, f))
	for {
		line, err := r.ReadString('\n')
		if strings.HasSuffix(line, "\n") {
			res.NewOffset += int64(len(line))
		}
		if errors.Is(err, io.EOF) {
			return res, nil
		}
		if err != nil {
			return res, err
		}
	}
}

// TestProcessFileReadsOverCapTailInWindows pins T-413: a seek-and-stream
// file whose unread tail is over MaxFileBytes is read in windows of the cap,
// one per pass, until it is caught up. Before, it was skipped on every pass
// forever, because the tail of a file the watcher fell behind on only grows
// (three transcripts 59 to 72 MB behind on the 2026-10-05 node).
func TestProcessFileReadsOverCapTailInWindows(t *testing.T) {
	root := t.TempDir()
	a := &stubChunkedAdapter{stubFlagAdapter: stubFlagAdapter{name: "stub", root: root}}
	const capBytes = 16
	w, s := quietWatcher(t, Options{MaxFileBytes: capBytes})

	path := filepath.Join(root, "big.stub")
	body := strings.Repeat("line0\n", 10) // 60 bytes, tail 60 > cap 16
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for pass := 0; pass < 10; pass++ {
		if err := w.processFile(ctx, a, path, false); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
	}
	off, err := s.GetCursor(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if off != int64(len(body)) {
		t.Fatalf("cursor = %d after the passes, want %d (caught up); adapter calls = %d", off, len(body), a.calls)
	}
	sawWindow := false
	for _, l := range a.limits {
		if l > capBytes {
			t.Fatalf("a parse was allowed %d bytes, over the %d cap", l, capBytes)
		}
		if l == capBytes {
			sawWindow = true
		}
	}
	if !sawWindow {
		t.Fatalf("no parse ran with a bounded window: limits %v", a.limits)
	}
}

// TestProcessFileChunkStallFallsBackToSkip pins the guard rail: a window that
// cannot make progress (one record longer than the cap) is not re-read on
// every pass; the file is skipped at that cursor like before.
func TestProcessFileChunkStallFallsBackToSkip(t *testing.T) {
	root := t.TempDir()
	a := &stubChunkedAdapter{stubFlagAdapter: stubFlagAdapter{name: "stub", root: root}}
	w, _ := quietWatcher(t, Options{MaxFileBytes: 16})

	path := filepath.Join(root, "huge-record.stub")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 40)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for pass := 0; pass < 3; pass++ {
		if err := w.processFile(ctx, a, path, false); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
	}
	if a.calls != 1 {
		t.Fatalf("adapter calls = %d, want 1 (the stalled window is not retried at the same cursor)", a.calls)
	}
}

// TestOversizeChunkedNeedsTheDeclaration pins that only a declared adapter
// reads its tail in windows; every other streaming file keeps the skip.
func TestOversizeChunkedNeedsTheDeclaration(t *testing.T) {
	if OversizeChunked(adapter.FileCursorSemantics{StreamsFromCursor: true}) {
		t.Fatal("an undeclared streaming adapter must keep the skip")
	}
	if !OversizeChunked(adapter.FileCursorSemantics{StreamsFromCursor: true, ChunkedTail: true}) {
		t.Fatal("a declared streaming adapter must read in windows")
	}
}
