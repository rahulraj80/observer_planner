package adapter

import (
	"context"
	"io"
)

// BOUNDED TAIL PARSES (T-413).
//
// The watcher's MaxFileBytes DoS guard bounds how much one parse may read.
// For a seek-and-stream file it compares the UNREAD TAIL against the cap,
// and before this seam it SKIPPED a file whose tail was over the cap. That
// is permanent capture loss the moment the watcher falls behind: the tail
// only grows, so the gate never reopens (the 2026-10-05 node had three
// transcripts frozen this way, 59 to 72 MB behind).
//
// An adapter that declares FileCursorSemantics.ChunkedTail instead honours a
// per-call byte limit carried on the parse context: it reads at most that
// many bytes past fromOffset and stops at the last complete record inside
// the window, which is exactly how it already stops at the current end of a
// file that is still being written. The watcher then catches up one window
// per pass, and one parse never reads more than the cap.

type parseByteLimitKey struct{}

// WithParseByteLimit returns ctx carrying a per-parse read limit of n bytes
// past the parse's fromOffset. n <= 0 means unlimited (ctx is returned
// unchanged). Only adapters that declared ChunkedTail read it.
func WithParseByteLimit(ctx context.Context, n int64) context.Context {
	if n <= 0 {
		return ctx
	}
	return context.WithValue(ctx, parseByteLimitKey{}, n)
}

// ParseByteLimit returns the read limit WithParseByteLimit put on ctx, or 0
// (unlimited) when none was set.
func ParseByteLimit(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	n, _ := ctx.Value(parseByteLimitKey{}).(int64)
	return n
}

// LimitTail wraps r (positioned at the parse's fromOffset) so it ends after
// the ctx's ParseByteLimit bytes. With no limit it returns r unchanged. A
// record cut by the limit reads exactly like an unterminated record at the
// end of a file that is still being written, which every ChunkedTail
// adapter already defers whole to its next parse.
func LimitTail(ctx context.Context, r io.Reader) io.Reader {
	if n := ParseByteLimit(ctx); n > 0 {
		return io.LimitReader(r, n)
	}
	return r
}

// TailChunkable reports whether the watcher may read this file's oversize
// tail in bounded windows instead of skipping it: the adapter declared
// ChunkedTail AND the delta gate applies (a seek-and-stream byte cursor).
func (s FileCursorSemantics) TailChunkable() bool {
	return s.ChunkedTail && s.DeltaGateMeaningful()
}
