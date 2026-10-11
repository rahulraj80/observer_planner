package procmodel

import (
	"testing"
	"time"
)

func TestCrossOSPoolTouched(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	sess := CrossOSSessionRef{SessionID: "s1", Tool: "claude-code", ProjectRoot: `C:\proj`, StartedAt: start}
	pool := NewCrossOSPool([]CrossOSProcRef{
		{ProcessKey: "root", StartedAt: start},
		{ProcessKey: "child", ParentProcessKey: "root", StartedAt: start.Add(time.Hour)},
		{ProcessKey: "orphan", ParentProcessKey: "not-yet-captured", StartedAt: start.Add(2 * time.Hour)},
	})
	later := start.Add(24 * time.Hour)
	cases := []struct {
		name  string
		delta CrossOSDelta
		want  bool
	}{
		{"unrelated churn", CrossOSDelta{ProcessKey: "x", ParentProcessKey: "explorer", StartedAt: later}, false},
		{"churn carrying another session", CrossOSDelta{ProcessKey: "x", SessionID: "s2", StartedAt: later}, false},
		{"already carries the session", CrossOSDelta{ProcessKey: "x", SessionID: "s1", StartedAt: later}, true},
		{"inside the anchor window", CrossOSDelta{ProcessKey: "x", StartedAt: start.Add(90 * time.Second)}, true},
		{"just before the window", CrossOSDelta{ProcessKey: "x", StartedAt: start.Add(-CrossOSWindow - time.Second)}, false},
		{"child of a pooled row", CrossOSDelta{ProcessKey: "x", ParentProcessKey: "child", StartedAt: later}, true},
		{"missing parent of a pooled row", CrossOSDelta{ProcessKey: "not-yet-captured", StartedAt: later}, true},
	}
	for _, tc := range cases {
		if got := pool.Touched(sess, []CrossOSDelta{tc.delta}, CrossOSWindow); got != tc.want {
			t.Errorf("%s: Touched = %v, want %v", tc.name, got, tc.want)
		}
	}
	if pool.Touched(sess, nil, CrossOSWindow) {
		t.Error("an empty delta touched the pool")
	}
	if pool.Len() != 3 {
		t.Errorf("Len = %d, want 3", pool.Len())
	}
}

func TestSameCrossOSSession(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	a := CrossOSSessionRef{SessionID: "s1", Tool: "codex", ProjectID: 3, ProjectRoot: "/p", StartedAt: start}
	b := a
	b.StartedAt = start.In(time.FixedZone("IST", 19800))
	if !SameCrossOSSession(a, b) {
		t.Error("the same instant in another zone is the same session")
	}
	b.ProjectRoot = "/q"
	if SameCrossOSSession(a, b) {
		t.Error("a changed project root must force a full pass")
	}
}
