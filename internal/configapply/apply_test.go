package configapply

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLeverForDetector(t *testing.T) {
	t.Parallel()
	// LeverForDetector returns the primary lever (first in list)
	lv := LeverForDetector("routing_shadow")
	if lv.Section != "routing" || lv.Key != "mode" || !lv.RestartRequired || lv.ChangeID != "mode" {
		t.Fatalf("routing_shadow primary lever = %+v, want {routing mode restart change_id=mode}", lv)
	}
	if got := LeverForDetector("trivial_model_overprovisioning"); got.Section != "" || got.Key != "" {
		t.Fatalf("trivial-model lever = %+v, want zero (behavioral, no config lever)", got)
	}
	if got := LeverForDetector(""); got.Section != "" {
		t.Fatalf("empty detector lever = %+v, want zero", got)
	}
}

func TestLeversForDetector(t *testing.T) {
	t.Parallel()
	levers := LeversForDetector("routing_shadow")
	if len(levers) != 3 {
		t.Fatalf("routing_shadow levers = %d, want 3", len(levers))
	}
	// Order: mode (primary), policy, stickiness
	want := []struct {
		Key        string
		ChangeID   string
		RestartReq bool
	}{
		{"mode", "mode", true},
		{"policy", "policy", true},
		{"stickiness.min_turns_between_switches", "stickiness", true},
	}
	for i, w := range want {
		if levers[i].Section != "routing" || levers[i].Key != w.Key || levers[i].ChangeID != w.ChangeID || levers[i].RestartRequired != w.RestartReq {
			t.Fatalf("lever[%d] = %+v, want {routing %s %v %s}", i, levers[i], w.Key, w.RestartReq, w.ChangeID)
		}
	}
	if got := LeversForDetector("trivial_model_overprovisioning"); len(got) != 0 {
		t.Fatalf("trivial-model levers = %v, want empty", got)
	}
	if got := LeversForDetector(""); len(got) != 0 {
		t.Fatalf("empty detector levers = %v, want empty", got)
	}
}

func TestBackupFileRestoreFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := "[routing]\npolicy = \"value\"\n# hand-edited comment\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := Stamp(time.Now())
	backup, err := BackupFile(path, stamp)
	if err != nil {
		t.Fatal(err)
	}
	if backup != path+BackupPrefix+stamp {
		t.Fatalf("backup path = %q, want %q", backup, path+BackupPrefix+stamp)
	}
	// The pre-write snapshot must be captured BEFORE the write so the
	// revert restores the exact state that existed before THIS write.
	if err := os.WriteFile(path, []byte("[routing]\npolicy = \"fast\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RestoreFile(path, backup); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("restored = %q, want %q (byte-exact, hand comments preserved)", got, original)
	}
}

func TestRestoreFileMissingBackup(t *testing.T) {
	t.Parallel()
	if err := RestoreFile(filepath.Join(t.TempDir(), "c.toml"), filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("missing backup must error")
	}
}

func TestStamp(t *testing.T) {
	t.Parallel()
	if got := Stamp(time.Date(2026, 8, 4, 13, 5, 7, 0, time.UTC)); got != "20260804T130507Z" {
		t.Fatalf("stamp = %q, want 20260804T130507Z", got)
	}
}
