package guard

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/policy"
)

// TestR101SymlinkParentGlobOnDisk pins Sol A2-04 through the real guard and
// a real symlink: `ln -s <home> ./out; rm -rf ./out/*` is the home
// directory's contents, whatever the operand looks like lexically.
func TestR101SymlinkParentGlobOnDisk(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	home := t.TempDir()
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(home, filepath.Join(proj, "out")); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default().Guard
	cfg.Enabled, cfg.Mode = true, "enforce"
	g, err := New(Options{Config: cfg, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	for cmd, want := range map[string]string{
		"rm -rf ./out/*": "R-101",
		"rm -rf ./out":   "R-101",
		"rm -rf ./build": "",
	} {
		v, gerr := g.Evaluate(policy.Event{
			Kind: policy.KindShellExec, Target: cmd, Cwd: proj, ProjectRoot: proj,
			SessionID: "s1", Now: time.Now().UTC(),
		})
		if gerr != nil {
			t.Fatal(gerr)
		}
		if v.RuleID != want && !(want == "" && v.RuleID != "R-101") {
			t.Errorf("%s: rule %q (%s), want %q", cmd, v.RuleID, v.Reason, want)
		}
	}
}

func TestResolveExistingLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	realDir, _ := filepath.EvalSymlinks(real)
	if got := resolveExistingLinks(filepath.ToSlash(filepath.Join(dir, "link", "*"))); got != filepath.ToSlash(filepath.Join(realDir, "*")) {
		t.Errorf("link/* = %q", got)
	}
	if got := resolveExistingLinks("relative/path"); got != "" {
		t.Errorf("relative = %q, want \"\"", got)
	}
	if got := resolveExistingLinks("c:/users/u"); runtime.GOOS != "windows" && got != "" {
		t.Errorf("a Windows-flavored path on this OS = %q, want \"\"", got)
	}
}
