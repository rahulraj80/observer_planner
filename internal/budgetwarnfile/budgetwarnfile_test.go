package budgetwarnfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestStatusRoundTripAndShownMarker pins the file contract the hooks and the
// statusline rely on: atomic write/read round-trip, missing file = nothing,
// and the shown marker makes each notice in-band exactly once.
func TestStatusRoundTripAndShownMarker(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "observer.db")
	sp := StatusPath(db)

	if f, err := Read(sp); err != nil || f != nil {
		t.Fatalf("missing file = (%v, %v), want (nil, nil)", f, err)
	}
	now := time.Date(2026, 10, 2, 5, 18, 50, 0, time.UTC)
	file := File{
		WrittenAt: now,
		Windows:   []Window{{Window: "daily", Unit: "usd", Period: "2026-10-02", Cap: 0.75, Spent: 1.26, Ratio: 1.68, Level: "exceeded", Crossed: 1}},
		Active: []Notice{
			{ID: "daily-usd-2026-10-02-1", Threshold: 1, CrossedAt: now, Message: "Org daily budget (you, warning only): 168% used"},
		},
	}
	if err := Write(sp, file); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sp)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("stat = %v %v, want 0600", info, err)
	}
	got, err := Read(sp)
	if err != nil || got == nil || len(got.Active) != 1 || got.Schema != Schema {
		t.Fatalf("read = %+v %v", got, err)
	}

	shown := ShownPath(db)
	pending := PendingInBand(got, ReadShown(shown), now.Add(time.Minute), 6*time.Hour)
	if len(pending) != 1 {
		t.Fatalf("pending = %+v", pending)
	}
	if err := MarkShown(shown, pending[0].ID); err != nil {
		t.Fatal(err)
	}
	if again := PendingInBand(got, ReadShown(shown), now.Add(2*time.Minute), 6*time.Hour); len(again) != 0 {
		t.Fatalf("a shown notice must not be shown again: %+v", again)
	}
	if stale := PendingInBand(&File{Active: []Notice{{ID: "x", CrossedAt: now}}}, Shown{}, now.Add(7*time.Hour), 6*time.Hour); len(stale) != 0 {
		t.Fatalf("an old crossing is not in-band news: %+v", stale)
	}

	if err := os.WriteFile(sp, []byte(`{"schema":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if f, err := Read(sp); err == nil || f != nil {
		t.Fatalf("foreign schema must be refused, got %+v", f)
	}
}

func TestMarkShownBounded(t *testing.T) {
	p := filepath.Join(t.TempDir(), ShownFilename)
	for i := 0; i < maxShownIDs+10; i++ {
		if err := MarkShown(p, string(rune('a'+i%26))+string(rune('0'+i/26))); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(ReadShown(p).IDs); n != maxShownIDs {
		t.Fatalf("marker holds %d ids, want %d", n, maxShownIDs)
	}
}

func TestTouchActivity(t *testing.T) {
	db := filepath.Join(t.TempDir(), "observer.db")
	if err := TouchActivity(db); err != nil {
		t.Fatal(err)
	}
	first, err := os.Stat(ActivityPath(db))
	if err != nil {
		t.Fatal(err)
	}
	old := first.ModTime().Add(-time.Hour)
	_ = os.Chtimes(ActivityPath(db), old, old)
	if err := TouchActivity(db); err != nil {
		t.Fatal(err)
	}
	second, _ := os.Stat(ActivityPath(db))
	if !second.ModTime().After(old) {
		t.Fatal("touch must advance the mtime")
	}
}
