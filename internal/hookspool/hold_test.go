package hookspool

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// TestDecide walks the disposition table one row per case (T-320).
func TestDecide(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	withRoot := Record{Tool: "t", SessionID: "s", ActionType: models.ActionSubagentStop, ProjectRoot: "/w", Timestamp: now}
	end := Record{Tool: "t", SessionID: "s", ActionType: models.ActionSessionEnd, ProjectRoot: "/w", Timestamp: now}
	noRoot := Record{Tool: "t", SessionID: "s", ActionType: models.ActionSubagentStop, Timestamp: now}
	comp := Record{Tool: "t", SessionID: "s", Kind: KindCompaction, Phase: "pre", Timestamp: now}
	old := comp
	old.Timestamp = now.Add(-MaxHoldAge - time.Second)
	tired := comp
	tired.Attempts = MaxHoldAttempts
	for _, tc := range []struct {
		name  string
		r     Record
		known bool
		want  Disposition
	}{
		{"a row with a root lands without a session", withRoot, false, Apply},
		{"a session end waits for its session", end, false, Hold},
		{"a session end lands once the session exists", end, true, Apply},
		{"a rootless row waits for its session", noRoot, false, Hold},
		{"a compaction waits for its session", comp, false, Hold},
		{"a compaction lands once the session exists", comp, true, Apply},
		{"an old record expires", old, false, Expire},
		{"an old record still lands if its session exists", old, true, Apply},
		{"a record held too many passes expires", tired, false, Expire},
	} {
		if got := Decide(tc.r, tc.known, now); got != tc.want {
			t.Errorf("%s: Decide = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestCompactionRecordVersioning: a compaction record is written as schema
// version 2 so a released drainer (version 1) drops it instead of ingesting
// an action with no action type; a tool-event row stays version 1.
func TestCompactionRecordVersioning(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), Filename)
	now := time.Now()
	if err := Append(p, Record{Tool: "t", SessionID: "s", Kind: KindCompaction, Phase: "pre", Timestamp: now}); err != nil {
		t.Fatal(err)
	}
	if err := Append(p, FromEvent(models.ToolEvent{Tool: "t", SessionID: "s", ActionType: models.ActionSessionEnd, Timestamp: now})); err != nil {
		t.Fatal(err)
	}
	if err := Append(p, Record{Tool: "t", SessionID: "s", Kind: "mystery", Phase: "pre"}); err == nil {
		t.Fatal("an unknown kind was accepted")
	}
	if _, err := SetAside(p, now); err != nil {
		t.Fatal(err)
	}
	ready, err := Ready(p, now.Add(Grace+time.Second))
	if err != nil || len(ready) != 1 || len(ready[0].Records) != 2 {
		t.Fatalf("ready = %+v err %v", ready, err)
	}
	if v := ready[0].Records[0].V; v != 2 {
		t.Errorf("compaction record version = %d, want 2", v)
	}
	if v := ready[0].Records[1].V; v != 1 {
		t.Errorf("tool-event record version = %d, want 1", v)
	}
}
