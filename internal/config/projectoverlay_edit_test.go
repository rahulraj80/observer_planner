package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestUpdateProjectOverlayKeepsOnlyWrittenKeys pins the T-421 item 1 writer
// fix: setting one profiles key in a fresh project file must not serialize
// the whole zero-valued [compression] block beside it. Before the fix the
// file came back with `enabled = false` for shell, indexing and conversation,
// which the proxy's ProjectCompression overlay applied, silently turning
// compression OFF for that project's traffic.
func TestUpdateProjectOverlayKeepsOnlyWrittenKeys(t *testing.T) {
	root := t.TempDir()
	if err := UpdateProjectOverlay(root, "profiles.default", "codex-variant"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ProjectOverlayFilename))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "compression") {
		t.Fatalf("a profiles-only edit wrote compression keys:\n%s", raw)
	}
	base := Default().Compression
	base.Conversation.Enabled = true
	base.Shell.Enabled = true
	got, err := ProjectCompression(base, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Conversation.Enabled || !got.Shell.Enabled {
		t.Fatalf("profiles-only edit changed compression: conversation=%v shell=%v", got.Conversation.Enabled, got.Shell.Enabled)
	}
	if pp := ProjectProfiles(defaultProfiles(), raw); pp.Default != "codex-variant" {
		t.Fatalf("profiles.default = %q", pp.Default)
	}
}

// TestPlanProjectOverlayEditPreservesUnrelatedKeys: an edit of one key keeps
// every other key the file already holds, including keys outside the
// allow-listed parse target (they are inert, but they are the author's).
func TestPlanProjectOverlayEditPreservesUnrelatedKeys(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ProjectOverlayFilename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[compression.conversation]\ntarget_ratio = 0.5\n\n[profiles.by_tool]\ncline = \"fast\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := PlanProjectOverlayEdit(ProjectOverlayEditRequest{Root: root, Key: "profiles.default", Value: "codex-variant"})
	if err != nil {
		t.Fatal(err)
	}
	if !e.Changed || !e.Existed || string(e.Before) != body {
		t.Fatalf("plan = changed %v existed %v before %q", e.Changed, e.Existed, e.Before)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if cur, _ := os.ReadFile(path); string(cur) != body {
		t.Fatal("planning wrote the file")
	}
	if err := ApplyProjectOverlayEdit(e); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	pp := ProjectProfiles(ProfilesConfig{}, raw)
	if pp.Default != "codex-variant" || pp.ByTool["cline"] != "fast" {
		t.Fatalf("profiles after edit = %+v", pp)
	}
	base := Default().Compression
	got, err := ProjectCompression(base, raw)
	if err != nil || got.Conversation.TargetRatio != 0.5 {
		t.Fatalf("target_ratio lost: %v %v", got.Conversation.TargetRatio, err)
	}
	if strings.Contains(string(raw), "shell") {
		t.Fatalf("edit introduced keys the file never had:\n%s", raw)
	}
}

// TestPlanProjectOverlayEditRemove: removing a key drops only that key,
// prunes the tables it empties, reports a no-op removal honestly, and an
// unknown key errors.
func TestPlanProjectOverlayEditRemove(t *testing.T) {
	root := t.TempDir()
	if err := UpdateProjectOverlay(root, "profiles.by_tool.cline", "fast"); err != nil {
		t.Fatal(err)
	}
	if err := UpdateProjectOverlay(root, "profiles.default", "codex-variant"); err != nil {
		t.Fatal(err)
	}
	e, err := PlanProjectOverlayEdit(ProjectOverlayEditRequest{Root: root, Key: "profiles.by_tool.cline", Remove: true})
	if err != nil {
		t.Fatal(err)
	}
	if !e.Changed {
		t.Fatal("removal of a present key must change the file")
	}
	if err := ApplyProjectOverlayEdit(e); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, ProjectOverlayFilename))
	if strings.Contains(string(raw), "by_tool") || !strings.Contains(string(raw), "codex-variant") {
		t.Fatalf("after removal:\n%s", raw)
	}
	again, err := PlanProjectOverlayEdit(ProjectOverlayEditRequest{Root: root, Key: "profiles.by_tool.cline", Remove: true})
	if err != nil || again.Changed {
		t.Fatalf("second removal: changed %v err %v", again.Changed, err)
	}
	if _, err := PlanProjectOverlayEdit(ProjectOverlayEditRequest{Root: root, Key: "profiles.nope", Remove: true}); err == nil {
		t.Fatal("unknown key removal must error")
	}
	if _, err := PlanProjectOverlayEdit(ProjectOverlayEditRequest{Root: root, Key: "observer.db_path", Remove: true}); err == nil {
		t.Fatal("daemon-level key must be refused")
	}
}

// TestPlanProjectOverlayEditRefusesUnsafeTargets: the project-file writer
// never writes the daemon's own config (home directory, or the resolved
// master config path) and never writes through a symbolic link a repository
// could have committed.
func TestPlanProjectOverlayEditRefusesUnsafeTargets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if _, err := PlanProjectOverlayEdit(ProjectOverlayEditRequest{Root: home, Key: "profiles.default", Value: "x"}); err == nil {
		t.Fatal("the home directory must be refused as a project root")
	}

	proj := t.TempDir()
	master := filepath.Join(proj, ProjectOverlayFilename)
	if _, err := PlanProjectOverlayEdit(ProjectOverlayEditRequest{Root: proj, Key: "profiles.default", Value: "x", ExcludeFile: master}); err == nil {
		t.Fatal("a root whose overlay path is the master config must be refused")
	}

	if _, err := PlanProjectOverlayEdit(ProjectOverlayEditRequest{Root: filepath.Join(proj, "missing"), Key: "profiles.default", Value: "x"}); err == nil {
		t.Fatal("a root that is not a directory must be refused")
	}

	if runtime.GOOS == "windows" {
		return
	}
	evil := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(evil, ".observer")); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanProjectOverlayEdit(ProjectOverlayEditRequest{Root: evil, Key: "profiles.default", Value: "x"}); err == nil {
		t.Fatal("a symlinked .observer directory must be refused")
	}
	if _, err := os.Stat(filepath.Join(target, "config.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the refused plan wrote through the symlink")
	}
}

// TestApplyProjectOverlayEditRefusesStalePlan: a file changed between the
// preview and the apply is not overwritten.
func TestApplyProjectOverlayEditRefusesStalePlan(t *testing.T) {
	root := t.TempDir()
	e, err := PlanProjectOverlayEdit(ProjectOverlayEditRequest{Root: root, Key: "profiles.default", Value: "fast"})
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdateProjectOverlay(root, "profiles.by_provider.openai", "codex-variant"); err != nil {
		t.Fatal(err)
	}
	if err := ApplyProjectOverlayEdit(e); !errors.Is(err, ErrProjectOverlayChanged) {
		t.Fatalf("stale plan apply = %v, want ErrProjectOverlayChanged", err)
	}
}

// TestReadProjectOverlayProfiles reports only the keys the file sets.
func TestReadProjectOverlayProfiles(t *testing.T) {
	root := t.TempDir()
	pc, exists, err := ReadProjectOverlayProfiles(root)
	if err != nil || exists || pc.Default != "" {
		t.Fatalf("absent file: %+v %v %v", pc, exists, err)
	}
	if err := UpdateProjectOverlay(root, "profiles.by_tool.cline", "fast"); err != nil {
		t.Fatal(err)
	}
	pc, exists, err = ReadProjectOverlayProfiles(root)
	if err != nil || !exists || pc.Default != "" || pc.ByTool["cline"] != "fast" || len(pc.ByProvider) != 0 {
		t.Fatalf("read: %+v %v %v", pc, exists, err)
	}
	if err := os.WriteFile(filepath.Join(root, ProjectOverlayFilename), []byte("not [valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := ReadProjectOverlayProfiles(root); err == nil || !exists {
		t.Fatalf("corrupt file: exists %v err %v", exists, err)
	}
}

// TestProfileTargetKey maps each target tier to its dotted key.
func TestProfileTargetKey(t *testing.T) {
	for _, tc := range []struct{ target, want string }{
		{"default", "profiles.default"},
		{"anthropic", "profiles.by_provider.anthropic"},
		{"tool:cline", "profiles.by_tool.cline"},
	} {
		pt, err := ParseProfileTarget(tc.target)
		if err != nil {
			t.Fatal(err)
		}
		if got := ProfileTargetKey(pt); got != tc.want {
			t.Errorf("%s -> %q, want %q", tc.target, got, tc.want)
		}
	}
}
