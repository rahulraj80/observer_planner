package configapply

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLedgerRecordAndRead(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l := NewLedger(path)
	c := Change{Section: "routing", Key: "policy", From: "value", To: "fast", RestartRequired: true}
	if err := l.RecordApply(c, "routing_shadow_1", "routing_shadow", 30.5, "s1", "config.toml"+BackupPrefix+"s1", "briefing-cta"); err != nil {
		t.Fatal(err)
	}
	if err := l.RecordRevert(c, "routing_shadow_1", "policy", "config.toml"+BackupPrefix+"s1", "briefing-cta"); err != nil {
		t.Fatal(err)
	}
	events, skipped, err := l.Read()
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 || len(events) != 2 {
		t.Fatalf("events=%d skipped=%d, want 2/0", len(events), skipped)
	}
	ev := events[0]
	if ev.Kind != "apply" || ev.Section != "routing" || ev.Key != "policy" ||
		ev.From != "value" || ev.To != "fast" || ev.DedupKey != "routing_shadow_1" ||
		ev.Detector != "routing_shadow" || ev.ProjectedUSD != 30.5 ||
		ev.Backup == "" || ev.Stamp != "s1" || ev.Time.IsZero() {
		t.Fatalf("apply event = %+v", ev)
	}
	if rv := events[1]; rv.Kind != "revert" || rv.From != "value" || rv.To != "fast" || rv.Backup == "" {
		t.Fatalf("revert event = %+v", rv)
	}
}

func TestLedgerMissingFileIsEmpty(t *testing.T) {
	t.Parallel()
	l := NewLedger(filepath.Join(t.TempDir(), "missing.jsonl"))
	events, skipped, err := l.Read()
	if err != nil || skipped != 0 || len(events) != 0 {
		t.Fatalf("missing ledger: events=%d skipped=%d err=%v", len(events), skipped, err)
	}
}

func TestLedgerSkipsGarbage(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	if err := os.WriteFile(path, []byte("{\"kind\":\"apply\"}\nnot json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := NewLedger(path)
	events, skipped, err := l.Read()
	if err != nil || skipped != 1 || len(events) != 1 {
		t.Fatalf("events=%d skipped=%d err=%v, want 1/1/nil", len(events), skipped, err)
	}
}

func TestLedgerLatestApply(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l := NewLedger(path)
	l.now = func() time.Time { return time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC) }
	c1 := Change{Section: "routing", Key: "policy", From: "value", To: "fast"}
	if err := l.RecordApply(c1, "a", "routing_shadow", 30, "s1", "cfg"+BackupPrefix+"s1", "t"); err != nil {
		t.Fatal(err)
	}
	l.now = func() time.Time { return time.Date(2026, 8, 4, 13, 0, 0, 0, time.UTC) }
	c2 := Change{Section: "routing", Key: "policy", From: "fast", To: "frugal"}
	if err := l.RecordApply(c2, "b", "routing_shadow", 40, "s2", "cfg"+BackupPrefix+"s2", "t"); err != nil {
		t.Fatal(err)
	}
	ev, ok, err := l.LatestApply("routing", "policy")
	if err != nil || !ok {
		t.Fatalf("latest = %+v ok=%v err=%v", ev, ok, err)
	}
	if ev.Stamp != "s2" || ev.To != "frugal" {
		t.Fatalf("latest apply = %+v, want the s2 (frugal) event", ev)
	}
	if _, ok, _ := l.LatestApply("guard", "mode"); ok {
		t.Fatal("wrong section must not resolve")
	}
	if _, ok, _ := l.LatestApply("routing", "tiers"); ok {
		t.Fatal("wrong key must not resolve")
	}
}

func TestLedgerAppliedBackupsDedup(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l := NewLedger(path)
	c := Change{Section: "routing", Key: "policy", From: "value", To: "fast"}
	if err := l.RecordApply(c, "a", "routing_shadow", 30, "s1", "cfg"+BackupPrefix+"s1", "t"); err != nil {
		t.Fatal(err)
	}
	// Same backup recorded twice (re-apply in one batch) must dedup.
	if err := l.RecordApply(c, "b", "routing_shadow", 30, "s1", "cfg"+BackupPrefix+"s1", "t"); err != nil {
		t.Fatal(err)
	}
	// A revert is not an apply and must not be returned.
	if err := l.RecordRevert(c, "b", "policy", "cfg"+BackupPrefix+"s1", "t"); err != nil {
		t.Fatal(err)
	}
	events, err := l.AppliedBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("applied backups = %d, want 1 (deduped)", len(events))
	}
}

// TestLedgerCardStateRevertKeying pins the invariant that a revert keyed by
// the apply's card + change_id flips that card's CardState back to
// not-applied — the per-card badge model depends on it. A lever-wide revert
// that drops the card linkage must NOT (the old all-revert path is tolerated
// for backward-compat but must not mask an apply).
func TestLedgerCardStateRevertKeying(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l := NewLedger(path)
	l.now = func() time.Time { return now }

	c := Change{Section: "routing", Key: "mode", From: "advise", To: "enforce", ChangeID: "mode"}
	if err := l.RecordApply(c, "routing_shadow_1", "routing_shadow", 20, "s1", "cfg"+BackupPrefix+"s1", "briefing-cta"); err != nil {
		t.Fatal(err)
	}
	st, err := l.CardState("routing_shadow_1", now)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Applied || len(st.AppliedLevers) != 1 || st.AppliedLevers[0].ChangeID != "mode" {
		t.Fatalf("after apply: %+v", st)
	}

	// Revert keyed by the apply's card + change_id flips the card.
	if err := l.RecordRevert(c, "routing_shadow_1", "mode", "cfg"+BackupPrefix+"s1", "briefing-cta"); err != nil {
		t.Fatal(err)
	}
	st, err = l.CardState("routing_shadow_1", now)
	if err != nil {
		t.Fatal(err)
	}
	if st.Applied || len(st.AppliedLevers) != 0 {
		t.Fatalf("after card-keyed revert: %+v", st)
	}

	// A lever-wide revert (no card linkage) must not flip the card.
	if err := l.RecordApply(c, "routing_shadow_1", "routing_shadow", 20, "s2", "cfg"+BackupPrefix+"s2", "briefing-cta"); err != nil {
		t.Fatal(err)
	}
	if err := l.RecordRevert(c, "", "", "cfg"+BackupPrefix+"s2", "briefing-cta"); err != nil {
		t.Fatal(err)
	}
	st, err = l.CardState("routing_shadow_1", now)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Applied || len(st.AppliedLevers) != 1 {
		t.Fatalf("after un-keyed revert: %+v", st)
	}
}
