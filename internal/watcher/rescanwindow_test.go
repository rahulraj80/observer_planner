package watcher

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/adapter"
	"github.com/marmutapp/superbased-observer/internal/adapter/codex"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// windowSpan is one bounded parse a rescan ran: [from, to).
type windowSpan struct{ from, to int64 }

// stubRescanAdapter is stubChunkedAdapter plus the span of every parse, so a
// test can prove the windows of one rescan cover the whole file.
type stubRescanAdapter struct {
	*stubChunkedAdapter
	spans []windowSpan
}

func (s *stubRescanAdapter) ParseSessionFile(ctx context.Context, path string, from int64) (adapter.ParseResult, error) {
	res, err := s.stubChunkedAdapter.ParseSessionFile(ctx, path, from)
	s.spans = append(s.spans, windowSpan{from: from, to: res.NewOffset})
	return res, err
}

// TestRescanReadsWholeOverCapFileInWindows pins T-568: a forced from-zero
// rescan (`observer backfill --codex-rescan`, `observer scan --force`) of a
// file larger than MaxFileBytes read ONE window from byte 0 and stopped. The
// persisted cursor is MAX-merged, so on a node whose cursor had already
// reached the end nothing ever re-read the rest: a 50 to 110 MB Codex rollout
// was rescanned only in its first 50 MB. A rescan now reads every window in
// one call, each still bounded by the cap.
func TestRescanReadsWholeOverCapFileInWindows(t *testing.T) {
	root := t.TempDir()
	a := &stubRescanAdapter{stubChunkedAdapter: &stubChunkedAdapter{stubFlagAdapter: stubFlagAdapter{name: "stub", root: root}}}
	const capBytes = 16
	w, s := quietWatcher(t, Options{MaxFileBytes: capBytes})

	path := filepath.Join(root, "big.stub")
	body := strings.Repeat("line0\n", 10) // 60 bytes, four 16-byte windows
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// The live daemon already caught up on this file (its cursor is at EOF).
	if err := s.SetCursor(ctx, path, int64(len(body))); err != nil {
		t.Fatal(err)
	}
	if err := w.processFile(ctx, a, path, true); err != nil {
		t.Fatal(err)
	}
	if len(a.spans) == 0 || a.spans[0].from != 0 {
		t.Fatalf("rescan did not start at byte 0: spans %v", a.spans)
	}
	var next int64
	for i, sp := range a.spans {
		if sp.from != next {
			t.Fatalf("window %d starts at %d, want %d (contiguous): spans %v", i, sp.from, next, a.spans)
		}
		next = sp.to
	}
	if next != int64(len(body)) {
		t.Fatalf("rescan windows ended at %d of %d bytes: spans %v", next, len(body), a.spans)
	}
	for _, l := range a.limits {
		if l <= 0 || l > capBytes {
			t.Fatalf("a rescan window was allowed %d bytes, want 1..%d", l, capBytes)
		}
	}
	off, err := s.GetCursor(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if off != int64(len(body)) {
		t.Fatalf("cursor = %d after the rescan, want %d (never regressed)", off, len(body))
	}
}

// TestRescanWindowsStopOnNoProgress pins the loop's exit: a record longer than
// the window makes no progress, and the rescan stops instead of spinning.
func TestRescanWindowsStopOnNoProgress(t *testing.T) {
	root := t.TempDir()
	a := &stubRescanAdapter{stubChunkedAdapter: &stubChunkedAdapter{stubFlagAdapter: stubFlagAdapter{name: "stub", root: root}}}
	w, _ := quietWatcher(t, Options{MaxFileBytes: 16})

	path := filepath.Join(root, "mixed.stub")
	body := "ok\n" + strings.Repeat("x", 40) + "\n" + "tail\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := w.processFile(context.Background(), a, path, true); err != nil {
		t.Fatal(err)
	}
	if a.calls != 2 {
		t.Fatalf("adapter calls = %d, want 2 (one window of progress, one stalled window): spans %v", a.calls, a.spans)
	}
}

// TestCodexRescanIngestsPromptPastFirstWindow is T-568 end to end on the real
// Codex parser: a rollout over MaxFileBytes whose second prompt sits past the
// first window. The live cursor is already at EOF (the daemon read the file
// long ago, with an older parser), so only the rescan can recover the prompt.
// Before the fix the rescan read the first window and stopped.
func TestCodexRescanIngestsPromptPastFirstWindow(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "w.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	s := store.New(database)

	sessionsRoot := filepath.Join(t.TempDir(), "sessions")
	if err := os.MkdirAll(sessionsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	rollout := filepath.Join(sessionsRoot, "rollout-2026-09-20T10-00-00-019dfbc2-344c-7a41-9766-9e6d97c28826.jsonl")
	reg := adapter.NewRegistry()
	reg.Register(codex.NewWithOptions(nil, sessionsRoot))
	const capBytes = 4096
	w := New(s, reg, Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), MaxFileBytes: capBytes})

	prompt := func(ts, text string) string {
		return `{"timestamp":"` + ts + `","type":"event_msg","payload":{"type":"user_message","message":"` + text + `"}}`
	}
	lines := []string{
		`{"timestamp":"2026-09-20T10:00:00.000Z","type":"session_meta","payload":{"id":"019dfbc2-344c-7a41-9766-9e6d97c28826","cwd":"/tmp/t568","model":"gpt-5.5"}}`,
		`{"timestamp":"2026-09-20T10:00:00.100Z","type":"turn_context","payload":{"turn_id":"t1","cwd":"/tmp/t568","model":"gpt-5.5"}}`,
		prompt("2026-09-20T10:00:00.200Z", "first prompt inside the first window"),
	}
	filler := `{"timestamp":"2026-09-20T10:00:01.000Z","type":"event_msg","payload":{"type":"token_count","info":null}}`
	for i := 0; i < 3*capBytes/len(filler)+1; i++ {
		lines = append(lines, filler)
	}
	lines = append(lines,
		`{"timestamp":"2026-09-20T10:05:00.000Z","type":"turn_context","payload":{"turn_id":"t2","cwd":"/tmp/t568","model":"gpt-5.5"}}`,
		prompt("2026-09-20T10:05:00.100Z", "late prompt past the first window"),
	)
	body := strings.Join(lines, "\n") + "\n"
	if len(body) < 3*capBytes {
		t.Fatalf("fixture is %d bytes, want over %d", len(body), 3*capBytes)
	}
	if err := os.WriteFile(rollout, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCursor(ctx, rollout, int64(len(body))); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Rescan(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM actions WHERE action_type = 'user_prompt' AND source_file = ?`, rollout).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("user_prompt rows after the rescan = %d, want 2 (the late prompt sits past the first %d-byte window)", n, capBytes)
	}
}
