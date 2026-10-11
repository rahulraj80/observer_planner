package diag

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/platform/crossmount"
)

// TestWindowsHomeTargets pins the one detection the doctor and the
// dashboard's Windows-home control share (T-421 item 4): every Windows home
// carrying a target's config dir is a candidate, exactly one owned home
// resolves, and a target with no candidate is omitted.
func TestWindowsHomeTargets(t *testing.T) {
	a, b, linux := t.TempDir(), t.TempDir(), t.TempDir()
	for _, d := range []string{filepath.Join(a, ".codex"), filepath.Join(b, ".codex"), filepath.Join(b, ".claude"), filepath.Join(linux, ".cursor")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	homes := []crossmount.HomeRoot{{Path: a, OS: crossmount.OSWindows}, {Path: b, OS: crossmount.OSWindows}, {Path: linux, OS: crossmount.OSLinux}}
	got := WindowsHomeTargets(homes, func(h string) bool { return h == b })
	byTarget := map[string]WindowsHomeTarget{}
	for _, tg := range got {
		byTarget[tg.Target] = tg
	}
	if len(got) != 2 {
		t.Fatalf("targets = %+v", got)
	}
	if c := byTarget["codex-windows"]; len(c.Candidates) != 2 || c.Resolved != b {
		t.Errorf("codex-windows = %+v", c)
	}
	if c := byTarget["claude-code-windows"]; len(c.Candidates) != 1 || c.Resolved != b || !c.Candidates[0].Owned {
		t.Errorf("claude-code-windows = %+v", c)
	}
	if _, ok := byTarget["cursor-windows"]; ok {
		t.Error("a Linux home's .cursor is not a Windows candidate")
	}
	if got := WindowsHomeTargets(homes, nil); got[0].Resolved != "" {
		t.Errorf("without an ownership seam nothing resolves: %+v", got)
	}
}
