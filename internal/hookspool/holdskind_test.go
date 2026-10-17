package hookspool

import (
	"path/filepath"
	"testing"
	"time"
)

// TestHoldsKindSeesLiveAndAsideFiles pins the P3-37 lookup: a held record is
// found on the live spool and on an aside file of any age, only for its own
// kind and session.
func TestHoldsKindSeesLiveAndAsideFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), Filename)
	now := time.Now().UTC()
	rec := Record{Kind: KindCompaction, Phase: "pre", Tool: "claude-code", SessionID: "s1", Timestamp: now}
	cases := []struct {
		name    string
		setup   func(t *testing.T)
		kind    string
		session string
		want    bool
	}{
		{"empty spool", func(*testing.T) {}, KindCompaction, "s1", false},
		{"live spool", func(t *testing.T) {
			if err := Append(path, rec); err != nil {
				t.Fatal(err)
			}
		}, KindCompaction, "s1", true},
		{"other session", func(*testing.T) {}, KindCompaction, "s2", false},
		{"other kind", func(*testing.T) {}, "", "s1", false},
		{"fresh aside file", func(t *testing.T) {
			if _, err := SetAside(path, now); err != nil {
				t.Fatal(err)
			}
		}, KindCompaction, "s1", true},
	}
	for _, c := range cases {
		c.setup(t)
		got, err := HoldsKind(path, c.kind, c.session)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: HoldsKind = %v, want %v", c.name, got, c.want)
		}
	}
}
