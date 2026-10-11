package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestReconcileBudgetCaptureInRootDirectorySymlinkIsCovered is node 1's Devin
// store (2026-10-06): ~/.local/share/devin/cli holds the vendor's own install
// layout, _versions/current -> 3000.11.3. That link points back into the tree
// the strict walk already reads, so nothing behind it is unread; reporting it
// as source_unreadable made Devin's accounting structurally unavailable and the
// process controller stopped every Devin launch under the member's managed USD
// cap ("accounting source unavailable for devin: source_unreadable").
func TestReconcileBudgetCaptureInRootDirectorySymlinkIsCovered(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		layout func(t *testing.T, root string) string // returns the link path
	}{
		{"relative sibling link (devin _versions/current)", func(t *testing.T, root string) string {
			if err := os.MkdirAll(filepath.Join(root, "_versions", "3000.11.3", "bin"), 0o700); err != nil {
				t.Fatal(err)
			}
			return mustSymlink(t, "3000.11.3", filepath.Join(root, "_versions", "current"))
		}},
		{"absolute link to a subdirectory", func(t *testing.T, root string) string {
			sub := filepath.Join(root, "plugins")
			if err := os.MkdirAll(sub, 0o700); err != nil {
				t.Fatal(err)
			}
			return mustSymlink(t, sub, filepath.Join(root, "plugins-link"))
		}},
		{"link back to the root itself", func(t *testing.T, root string) string {
			return mustSymlink(t, ".", filepath.Join(root, "self"))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeBudgetSource(t, root, "source")
			tc.layout(t, root)
			w, _ := newBudgetCaptureTestWatcher(t, Options{}, &budgetFakeAdapter{name: "fake", root: root})

			got := w.ReconcileBudgetCapture(context.Background(), []string{"fake"})["fake"]
			if got.Reason == BudgetCaptureReasonUnreadable || !got.AccountingReady() {
				t.Fatalf("status = %+v, want an in-root directory symlink to leave the source readable", got)
			}
			if !got.Ready || got.Reason != BudgetCaptureReasonReady {
				t.Fatalf("status = %+v, want ready", got)
			}
		})
	}
}

func mustSymlink(t *testing.T, target, link string) string {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	return link
}
