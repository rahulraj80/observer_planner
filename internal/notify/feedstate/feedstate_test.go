package feedstate_test

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/notify/feedstate"
)

// TestDismissPersists: a dismissal survives a new Store over the same file
// (a daemon restart, or another browser on the same node).
func TestDismissPersists(t *testing.T) {
	path := feedstate.Path(filepath.Join(t.TempDir(), "observer.db"))
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	if err := feedstate.New(path).Mark([]string{"a", "b"}, feedstate.ActionRead, now); err != nil {
		t.Fatal(err)
	}
	if err := feedstate.New(path).Mark([]string{"b"}, feedstate.ActionDismiss, now); err != nil {
		t.Fatal(err)
	}
	got := feedstate.New(path).Load()
	if !got["a"].Read || got["a"].Dismissed {
		t.Fatalf("a = %+v, want read only", got["a"])
	}
	if !got["b"].Read || !got["b"].Dismissed {
		t.Fatalf("b = %+v, want read and dismissed", got["b"])
	}
	if _, ok := got["c"]; ok {
		t.Fatal("unknown id has state")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600", info.Mode().Perm())
	}
}

func TestUnknownActionAndEmptyPath(t *testing.T) {
	s := feedstate.New(filepath.Join(t.TempDir(), feedstate.Filename))
	if err := s.Mark([]string{"a"}, "archive", time.Now()); !errors.Is(err, feedstate.ErrUnknownAction) {
		t.Fatalf("err = %v, want ErrUnknownAction", err)
	}
	none := feedstate.New("")
	if err := none.Mark([]string{"a"}, feedstate.ActionRead, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(none.Load()) != 0 {
		t.Fatal("empty path kept state")
	}
}

// TestCorruptFileIsNoState: an unreadable file never errors the bell; the
// next write replaces it.
func TestCorruptFileIsNoState(t *testing.T) {
	path := filepath.Join(t.TempDir(), feedstate.Filename)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := feedstate.New(path)
	if len(s.Load()) != 0 {
		t.Fatal("corrupt file produced state")
	}
	if err := s.Mark([]string{"x"}, feedstate.ActionRead, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !s.Load()["x"].Read {
		t.Fatal("write after corruption lost")
	}
}

// TestPruneBounds: entries past Retention age out on the next write, and the
// file never holds more than MaxEntries.
func TestPruneBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), feedstate.Filename)
	s := feedstate.New(path)
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := s.Mark([]string{"old"}, feedstate.ActionDismiss, old); err != nil {
		t.Fatal(err)
	}
	now := old.Add(feedstate.Retention + time.Hour)
	ids := make([]string, feedstate.MaxEntries+10)
	for i := range ids {
		ids[i] = "n" + strconv.Itoa(i)
	}
	if err := s.Mark(ids, feedstate.ActionRead, now); err != nil {
		t.Fatal(err)
	}
	got := s.Load()
	if _, ok := got["old"]; ok {
		t.Fatal("expired entry kept")
	}
	if len(got) > feedstate.MaxEntries {
		t.Fatalf("%d entries, want at most %d", len(got), feedstate.MaxEntries)
	}
}
