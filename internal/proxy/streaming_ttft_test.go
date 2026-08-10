package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// splitReader yields body in fixed-size chunks across multiple Read calls so
// a test can exercise multi-chunk stream capture.
type splitReader struct {
	data  []byte
	off   int
	chunk int
}

func (r *splitReader) Read(b []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := len(r.data) - r.off
	if n > r.chunk {
		n = r.chunk
	}
	if n > len(b) {
		n = len(b)
	}
	copy(b, r.data[r.off:r.off+n])
	r.off += n
	return n, nil
}

// TestTeeStream_FirstChunkTime pins the TTFT capture seam: the returned time
// is the stamp of the FIRST chunk that arrived (and only the first — later
// chunks never re-stamp it), and the full body is still forwarded + captured.
func TestTeeStream_FirstChunkTime(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	base := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	tick := 0
	// now() advances 10ms per call so a wrong "last chunk" stamp is
	// distinguishable from the expected "first chunk" stamp.
	p := &Proxy{now: func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		tick++
		return base.Add(time.Duration(tick) * 10 * time.Millisecond)
	}}

	body := []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n")
	rd := &splitReader{data: body, chunk: len(body) / 2}

	rec := httptest.NewRecorder()
	captured, first := p.teeStream(context.Background(), rec, rd)
	if string(captured) != string(body) {
		t.Errorf("captured = %q, want full body", captured)
	}
	if !first.Equal(base.Add(10 * time.Millisecond)) {
		t.Errorf("firstChunkAt = %v, want %v (the first read's stamp)", first, base.Add(10*time.Millisecond))
	}
	if rec.Body.String() != string(body) {
		t.Errorf("forwarded body = %q, want full body", rec.Body.String())
	}
}

// TestTeeStream_EmptyStreamNoStamp — a stream that delivers no chunk returns
// a zero firstChunkAt, so callers can distinguish "no TTFT observation" from
// a real (even 0ms) first-token time.
func TestTeeStream_EmptyStreamNoStamp(t *testing.T) {
	t.Parallel()
	p := &Proxy{now: func() time.Time { return time.Unix(0, 0) }}
	rec := httptest.NewRecorder()
	captured, first := p.teeStream(context.Background(), rec, strings.NewReader(""))
	if len(captured) != 0 {
		t.Errorf("captured = %q, want empty", captured)
	}
	if !first.IsZero() {
		t.Errorf("firstChunkAt = %v, want zero for an empty stream", first)
	}
}

// TestBuildStreamTurn_TTFT pins the conversion from first-chunk stamp to the
// APITurn's TimeToFirstTokenMS, and that a zero stamp (buffered capture, e.g.
// the sniffed-SSE fallback) leaves TTFT unset rather than faking a value.
func TestBuildStreamTurn_TTFT(t *testing.T) {
	t.Parallel()
	p := &Proxy{now: func() time.Time { return time.Unix(0, 0) }}
	start := time.Unix(0, 0)
	firstChunk := start.Add(500 * time.Millisecond)
	sse := []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n")
	req := requestShape{Model: "claude-sonnet-4"}

	turn := p.buildStreamTurn(models.ProviderAnthropic, req, sse, http.Header{}, start, firstChunk, "")
	if turn.TimeToFirstTokenMS != 500 {
		t.Errorf("TTFT = %dms, want 500ms", turn.TimeToFirstTokenMS)
	}
	if turn.TotalResponseMS != 0 {
		t.Errorf("TotalResponseMS = %dms, want 0 (clock fixed at start)", turn.TotalResponseMS)
	}

	zero := p.buildStreamTurn(models.ProviderAnthropic, req, sse, http.Header{}, start, time.Time{}, "")
	if zero.TimeToFirstTokenMS != 0 {
		t.Errorf("TTFT with zero firstChunkAt = %dms, want 0 (buffered capture asserts no TTFT)", zero.TimeToFirstTokenMS)
	}
}
