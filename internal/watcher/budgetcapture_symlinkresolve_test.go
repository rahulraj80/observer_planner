package watcher

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBudgetSymlinkInsideRoot is the resolver table behind the walk's
// decision. A link that leaves the root stays outside it (the existing
// TestReconcileBudgetCaptureRejectsNestedDirectorySymlink keeps that case
// failing closed end to end).
func TestBudgetSymlinkInsideRoot(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	root := filepath.Join(parent, "cli")
	inside := filepath.Join(root, "_versions", "3000.11.3")
	outside := filepath.Join(parent, "elsewhere")
	sibling := filepath.Join(parent, "cli-other") // shares the root's name prefix
	for _, d := range []string{inside, outside, sibling} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	rows := []struct {
		name   string
		target string
		want   bool
	}{
		{"relative inside", "3000.11.3", true},
		{"absolute inside", inside, true},
		{"the root itself", root, true},
		{"outside the root", outside, false},
		{"a sibling sharing the root's name prefix", sibling, false},
		{"dangling", filepath.Join(root, "missing"), false},
	}
	for i, r := range rows {
		link := filepath.Join(root, "_versions", "link"+string(rune('a'+i)))
		mustSymlink(t, r.target, link)
		if got := budgetSymlinkInsideRoot(root, link); got != r.want {
			t.Errorf("%s: budgetSymlinkInsideRoot = %v, want %v", r.name, got, r.want)
		}
	}
}
