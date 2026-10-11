package agentconfigsvc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/agentconfig"
	"github.com/marmutapp/superbased-observer/internal/store"
)

type fakeStore struct {
	mu     sync.Mutex
	layers map[string]store.AgentConfigLayerRow
	writes []store.AgentConfigWriteRow
}

func newFakeStore() *fakeStore { return &fakeStore{layers: map[string]store.AgentConfigLayerRow{}} }

func (f *fakeStore) RecordAgentConfigSave(_ context.Context, layers []store.AgentConfigLayerRow, w store.AgentConfigWriteRow) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range layers {
		k := l.FilePath + "\x00" + l.KeyPath
		if l.Delete {
			delete(f.layers, k)
			continue
		}
		f.layers[k] = l
	}
	f.writes = append(f.writes, w)
	return int64(len(f.writes)), nil
}

func (f *fakeStore) LoadAgentConfigLayers(_ context.Context, paths []string) ([]store.AgentConfigLayerRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	want := map[string]bool{}
	for _, p := range paths {
		want[p] = true
	}
	var out []store.AgentConfigLayerRow
	for _, l := range f.layers {
		if len(paths) == 0 || want[l.FilePath] {
			out = append(out, l)
		}
	}
	return out, nil
}

func (f *fakeStore) ListAgentConfigWrites(_ context.Context, tool string, _ int) ([]store.AgentConfigWriteRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.AgentConfigWriteRow
	for i := len(f.writes) - 1; i >= 0; i-- {
		if tool == "" || f.writes[i].Tool == tool {
			out = append(out, f.writes[i])
		}
	}
	return out, nil
}

type harness struct {
	t       *testing.T
	home    string
	project string
	st      *fakeStore
	locks   []string
	svc     *Service
}

func newHarness(t *testing.T, mod func(*Options)) *harness {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("paths in these tests are POSIX")
	}
	h := &harness{t: t, home: t.TempDir(), project: t.TempDir(), st: newFakeStore()}
	n := 0
	o := Options{
		Home: h.home, GOOS: "linux", Getenv: func(string) string { return "" }, SystemRoot: filepath.Join(h.home, "sysroot"),
		Store: h.st, ProxyAddrs: []string{"127.0.0.1:8820"},
		Lock: func(p string) (func(), error) {
			h.locks = append(h.locks, p)
			return func() {}, nil
		},
		Now: func() time.Time { n++; return time.Date(2026, 10, 4, 9, 0, n, 0, time.UTC) },
	}
	if mod != nil {
		mod(&o)
	}
	h.svc = New(o)
	return h
}

func (h *harness) write(rel, content string) string {
	h.t.Helper()
	p := filepath.Join(h.home, rel)
	if strings.HasPrefix(rel, "<project>/") {
		p = filepath.Join(h.project, strings.TrimPrefix(rel, "<project>/"))
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
	return p
}

func (h *harness) read(p string) string {
	h.t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		h.t.Fatal(err)
	}
	return string(b)
}

const claudeSettings = `{
  "model": "sonnet",
  "env": {"OPENAI_API_KEY": "plain-secret-value"},
  "hooks": {"PreToolUse": [{"matcher": "*", "hooks": [{"type": "command", "command": "observer hook claude-code pre-tool"}]}]}
}
`

func (h *harness) saveForm(edits []agentconfig.Edit, confirm bool) (SaveOutcome, error) {
	req := SaveRequest{Target: Target{Tool: "claude-code", FileID: "claude-code/user-settings"}, Edits: edits}
	p, err := h.svc.Preview(context.Background(), req)
	if err != nil {
		return SaveOutcome{}, err
	}
	req.PreviewEtag, req.Confirm = p.PreviewEtag, confirm
	return h.svc.Save(context.Background(), req)
}

func TestInspectNeverReadsPrivNever(t *testing.T) {
	h := newHarness(t, nil)
	h.write(".claude/settings.json", claudeSettings)
	h.write(".claude.json", `{"oauthAccount": {"token": "x"}}`)
	v, err := h.svc.Inspect(context.Background(), "claude-code", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range v.Files {
		if f.ID == "claude-code/global-config" {
			if f.State != agentconfig.FileNeverRead || f.Size != 0 || f.Path != "" {
				t.Fatalf("never-read file was touched: %+v", f)
			}
		}
		if f.ID == "claude-code/user-settings" && (f.State != agentconfig.FilePresent || len(f.Secrets) != 1) {
			t.Fatalf("user settings: %+v", f)
		}
	}
	if _, err := h.svc.Raw(context.Background(), Target{Tool: "claude-code", FileID: "claude-code/global-config"}); !errors.Is(err, ErrNotEditable) {
		t.Fatalf("raw of a never-read file: %v", err)
	}
	if _, err := h.svc.Inspect(context.Background(), "no-such-tool", ""); !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("unknown tool: %v", err)
	}
}

func TestSaveFormEdit(t *testing.T) {
	h := newHarness(t, nil)
	p := h.write(".claude/settings.json", claudeSettings)
	out, err := h.saveForm([]agentconfig.Edit{{Path: agentconfig.KeyPath{"model"}, Op: agentconfig.OpSet, Value: "opus"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Saved || out.BackupPath == "" {
		t.Fatalf("outcome %+v", out)
	}
	got := h.read(p)
	if !strings.Contains(got, `"model": "opus"`) || !strings.Contains(got, "plain-secret-value") || !strings.Contains(got, "observer hook") {
		t.Fatalf("file after save:\n%s", got)
	}
	if h.read(out.BackupPath) != claudeSettings {
		t.Fatal("backup does not hold the previous bytes")
	}
	if len(h.locks) == 0 || h.locks[0] != p {
		t.Fatalf("lock not taken on %s: %v", p, h.locks)
	}
	if len(h.st.writes) != 1 || h.st.writes[0].Layer != "l2_adapter" || h.st.writes[0].ChangedKeys[0] != "model" {
		t.Fatalf("history %+v", h.st.writes)
	}
	// The badge: the view reads the layer back.
	v, _ := h.svc.Inspect(context.Background(), "claude-code", "")
	for _, s := range v.Settings {
		if s.ID == "claude-code/model" && (s.Effective.Value != "opus" || s.Effective.Layer != agentconfig.ValueL2) {
			t.Fatalf("model view %+v", s.Effective)
		}
	}
}

func TestSaveRefusals(t *testing.T) {
	h := newHarness(t, nil)
	p := h.write(".claude/settings.json", claudeSettings)
	ctx := context.Background()

	// Stale: the file moves between preview and save.
	req := SaveRequest{
		Target: Target{Tool: "claude-code", FileID: "claude-code/user-settings"},
		Edits:  []agentconfig.Edit{{Path: agentconfig.KeyPath{"model"}, Op: agentconfig.OpSet, Value: "opus"}},
	}
	pv, _ := h.svc.Preview(ctx, req)
	h.write(".claude/settings.json", strings.Replace(claudeSettings, "sonnet", "haiku", 1))
	req.PreviewEtag = pv.PreviewEtag
	if _, err := h.svc.Save(ctx, req); !errors.Is(err, ErrStale) {
		t.Fatalf("stale save: %v", err)
	}

	// Confirm required for a dangerous value.
	h.write(".claude/settings.json", claudeSettings)
	if _, err := h.saveForm([]agentconfig.Edit{{Path: agentconfig.KeyPath{"permissions", "defaultMode"}, Op: agentconfig.OpSet, Value: "bypassPermissions"}}, false); !errors.Is(err, ErrConfirm) {
		t.Fatalf("unconfirmed dangerous save: %v", err)
	}
	if _, err := h.saveForm([]agentconfig.Edit{{Path: agentconfig.KeyPath{"permissions", "defaultMode"}, Op: agentconfig.OpSet, Value: "bypassPermissions"}}, true); err != nil {
		t.Fatalf("confirmed dangerous save: %v", err)
	}

	// Observer's hook entry cannot be removed.
	_, err := h.saveForm([]agentconfig.Edit{{Path: agentconfig.KeyPath{"hooks"}, Op: agentconfig.OpDelete}}, true)
	var se *SaveError
	if !errors.As(err, &se) || !errors.Is(err, ErrBlocked) {
		t.Fatalf("hook removal: %v", err)
	}
	if !strings.Contains(h.read(p), "observer hook") {
		t.Fatal("the hook was removed despite the refusal")
	}
}

func TestManagedNodeIsReadOnly(t *testing.T) {
	h := newHarness(t, func(o *Options) {
		o.Access = func(context.Context) agentconfig.Access {
			return agentconfig.Access{ReadOnly: true, Reason: "Read-only until your organization grants editing."}
		}
	})
	p := h.write(".claude/settings.json", claudeSettings)
	if _, err := h.saveForm([]agentconfig.Edit{{Path: agentconfig.KeyPath{"model"}, Op: agentconfig.OpSet, Value: "opus"}}, true); !errors.Is(err, ErrBlocked) {
		t.Fatalf("managed save: %v", err)
	}
	if h.read(p) != claudeSettings {
		t.Fatal("a read-only node wrote the file")
	}
}

func TestRawRoundTripKeepsSecrets(t *testing.T) {
	h := newHarness(t, nil)
	p := h.write(".claude/settings.json", claudeSettings)
	ctx := context.Background()
	rv, err := h.svc.Raw(ctx, Target{Tool: "claude-code", FileID: "claude-code/user-settings"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rv.Text, "plain-secret-value") || len(rv.Masked) != 1 {
		t.Fatalf("raw view leaks: %+v", rv)
	}
	edited := strings.Replace(rv.Text, `"sonnet"`, `"opus"`, 1)
	req := SaveRequest{Target: Target{Tool: "claude-code", FileID: "claude-code/user-settings"}, Raw: &edited}
	pv, err := h.svc.Preview(ctx, req)
	if err != nil || !pv.OK {
		t.Fatalf("preview: %v %+v", err, pv.Findings)
	}
	if strings.Contains(pv.Diff, "plain-secret-value") {
		t.Fatal("diff leaks the secret")
	}
	req.PreviewEtag = pv.PreviewEtag
	if _, err := h.svc.Save(ctx, req); err != nil {
		t.Fatal(err)
	}
	got := h.read(p)
	if !strings.Contains(got, "plain-secret-value") || !strings.Contains(got, `"opus"`) {
		t.Fatalf("after raw save:\n%s", got)
	}
}

func TestProjectLocalCreateAddsGitExclude(t *testing.T) {
	h := newHarness(t, nil)
	if err := os.MkdirAll(filepath.Join(h.project, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	req := SaveRequest{
		Target: Target{Tool: "claude-code", FileID: "claude-code/project-local-settings", Project: h.project},
		Edits:  []agentconfig.Edit{{Path: agentconfig.KeyPath{"model"}, Op: agentconfig.OpSet, Value: "opus"}},
	}
	pv, err := h.svc.Preview(ctx, req)
	if err != nil || !pv.OK || !pv.Created {
		t.Fatalf("preview: %v %+v", err, pv)
	}
	req.PreviewEtag = pv.PreviewEtag
	out, err := h.svc.Save(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Excluded != "/.claude/settings.local.json" {
		t.Fatalf("excluded = %q", out.Excluded)
	}
	ex := h.read(filepath.Join(h.project, ".git", "info", "exclude"))
	if !strings.Contains(ex, "/.claude/settings.local.json") {
		t.Fatalf("exclude:\n%s", ex)
	}
	if !out.Created || h.st.writes[0].Layer != "l3_project" {
		t.Fatalf("outcome %+v history %+v", out, h.st.writes)
	}
}

func TestSymlinkWriteThrough(t *testing.T) {
	h := newHarness(t, nil)
	real := h.write("dotfiles/claude.json", claudeSettings)
	link := filepath.Join(h.home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := h.saveForm([]agentconfig.Edit{{Path: agentconfig.KeyPath{"model"}, Op: agentconfig.OpSet, Value: "opus"}}, false); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced by a file")
	}
	if !strings.Contains(h.read(real), `"opus"`) {
		t.Fatal("the link target was not written")
	}
}

func TestBackupRing(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.BackupKeep = 2 })
	p := h.write(".claude/settings.json", claudeSettings)
	for _, m := range []string{"a", "b", "c", "d"} {
		if _, err := h.saveForm([]agentconfig.Edit{{Path: agentconfig.KeyPath{"model"}, Op: agentconfig.OpSet, Value: m}}, false); err != nil {
			t.Fatal(err)
		}
	}
	matches, _ := filepath.Glob(p + BackupPrefix + "*")
	if len(matches) != 2 {
		t.Fatalf("backups = %v", matches)
	}
}

// TestCommonFanOutRollbackRecordsNothing pins review M3-08: a fan-out that
// fails part-way restores the files it wrote and records NO history or layer
// row for them (history used to list saves that were rolled back).
func TestCommonFanOutRollbackRecordsNothing(t *testing.T) {
	h := newHarness(t, nil)
	cp := h.write(".claude/settings.json", claudeSettings)
	if err := os.MkdirAll(filepath.Join(h.home, ".kimi-code"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	req := CommonRequest{Key: agentconfig.KeyTheme, Value: "dark"}
	pv, err := h.svc.CommonPreview(ctx, req)
	if err != nil || !pv.OK || pv.Writes < 2 {
		t.Fatalf("common preview: %+v %v", pv, err)
	}
	req.PreviewEtag = pv.PreviewEtag
	calls := 0
	h.svc.commitHook = func(pin pinnedTarget, p Preview) (written, error) {
		calls++
		if calls == pv.Writes {
			return written{}, errors.New("disk full")
		}
		return h.svc.commitFile(pin, p)
	}
	before := h.read(cp)
	_, err = h.svc.CommonSave(ctx, req)
	if err == nil || !strings.Contains(err.Error(), "every file written before it was restored") {
		t.Fatalf("CommonSave err = %v, want the honest restored message", err)
	}
	h.st.mu.Lock()
	nw, nl := len(h.st.writes), len(h.st.layers)
	h.st.mu.Unlock()
	if nw != 0 || nl != 0 {
		t.Fatalf("a rolled-back fan-out recorded %d history rows and %d layer rows", nw, nl)
	}
	if h.read(cp) != before {
		t.Fatalf("claude settings not restored:\n%s", h.read(cp))
	}
}

// TestRollbackAllSaysWhatFailed: a restore that itself fails is named, never
// reported as restored.
func TestRollbackAllSaysWhatFailed(t *testing.T) {
	h := newHarness(t, nil)
	// The parent is a regular file, so the restore cannot be written even by
	// a privileged user.
	notDir := h.write("not-a-dir", "x")
	missing := filepath.Join(notDir, "settings.json")
	msg := h.svc.rollbackAll([]written{{path: missing, before: []byte("{}"), mode: 0o600}})
	if !strings.Contains(msg, "could not restore "+missing) {
		t.Fatalf("rollbackAll = %q", msg)
	}
	if msg := h.svc.rollbackAll(nil); msg != "every file written before it was restored" {
		t.Fatalf("rollbackAll(nil) = %q", msg)
	}
}

func TestCommonFanOut(t *testing.T) {
	h := newHarness(t, nil)
	cp := h.write(".claude/settings.json", claudeSettings)
	if err := os.MkdirAll(filepath.Join(h.home, ".kimi-code"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	req := CommonRequest{Key: agentconfig.KeyTheme, Value: "dark"}
	pv, err := h.svc.CommonPreview(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !pv.OK || pv.Writes < 2 {
		t.Fatalf("common preview: %+v", pv)
	}
	req.PreviewEtag = pv.PreviewEtag
	out, err := h.svc.CommonSave(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Saved || len(out.Written) < 2 {
		t.Fatalf("common outcome: %+v", out)
	}
	if _, err := os.Stat(filepath.Join(h.home, ".kimi-code", "config.toml")); !os.IsNotExist(err) {
		t.Fatal("the never-read kimi config.toml was created")
	}
	if !strings.Contains(h.read(cp), `"theme"`) {
		t.Fatalf("claude settings:\n%s", h.read(cp))
	}
	l1 := 0
	for _, w := range h.st.writes {
		if w.Layer == "l1_all_adapters" && w.CommonKey == "theme" {
			l1++
		}
	}
	if l1 != len(out.Written) {
		t.Fatalf("history L1 rows %d, written %d", l1, len(out.Written))
	}
	views, err := h.svc.CommonView(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range views {
		if kv.Spec.Key != agentconfig.KeyTheme {
			continue
		}
		set := 0
		for _, tv := range kv.Tools {
			if tv.Set && tv.Value == "dark" && tv.Layer == agentconfig.ValueL1 {
				set++
			}
		}
		if set < 2 {
			t.Fatalf("theme view: %+v", kv.Tools)
		}
	}
}

func TestInstructions(t *testing.T) {
	h := newHarness(t, nil)
	h.write("<project>/CLAUDE.md", "# project notes\n")
	h.write("<project>/CLAUDE.local.md", "# my notes\n")
	h.write(".claude/skills/review/SKILL.md", "---\nname: review\ndescription: reviews\n---\nbody\n")
	ctx := context.Background()
	files, err := h.svc.Instructions(ctx, "claude-code", h.project)
	if err != nil {
		t.Fatal(err)
	}
	var proj, local, skill, newSkill bool
	for _, f := range files {
		switch {
		case f.Path == filepath.Join(h.project, "CLAUDE.md") && f.Exists:
			// Shared with the team: a save is carried by the next commit.
			proj = f.Scope == agentconfig.ScopeProjectShared && f.Tracked
		case f.Path == filepath.Join(h.project, "CLAUDE.local.md") && f.Exists:
			// Personal and gitignored: never "commits with the repo".
			local = f.Scope == agentconfig.ScopeProjectLocal && !f.Tracked
		case strings.HasSuffix(f.Path, "review/SKILL.md") && f.Exists && f.Kind == agentconfig.KindSkill:
			skill = true
		case f.NewName && f.Kind == agentconfig.KindSkill:
			newSkill = true
		}
	}
	if !proj || !local || !skill || !newSkill {
		t.Fatalf("proj=%v local=%v skill=%v new=%v files=%+v", proj, local, skill, newSkill, files)
	}
	// Create a new user skill through the pipeline.
	var row string
	for _, f := range files {
		if f.NewName && f.Kind == agentconfig.KindSkill && f.Scope == agentconfig.ScopeUser {
			row = f.FileID
		}
	}
	path, err := h.svc.NewInstructionPath(Target{Tool: "claude-code", FileID: row}, "deploy")
	if err != nil {
		t.Fatal(err)
	}
	body := "---\nname: deploy\ndescription: ships it\n---\nSteps.\n"
	req := SaveRequest{Target: Target{Tool: "claude-code", FileID: row, Path: path}, Raw: &body}
	pv, err := h.svc.Preview(ctx, req)
	if err != nil || !pv.OK {
		t.Fatalf("skill preview: %v %+v", err, pv.Findings)
	}
	req.PreviewEtag = pv.PreviewEtag
	if _, err := h.svc.Save(ctx, req); err != nil {
		t.Fatal(err)
	}
	if h.read(path) != body {
		t.Fatal("skill not written")
	}
	// A path outside the glob is refused.
	bad := filepath.Join(h.home, "elsewhere.md")
	if _, err := h.svc.Preview(ctx, SaveRequest{Target: Target{Tool: "claude-code", FileID: row, Path: bad}, Raw: &body}); !errors.Is(err, ErrBadTarget) {
		t.Fatalf("outside path: %v", err)
	}
	if _, err := h.svc.NewInstructionPath(Target{Tool: "claude-code", FileID: row}, "../x"); !errors.Is(err, ErrBadTarget) {
		t.Fatalf("bad name: %v", err)
	}
}

// TestCommonFanOutAtProject is T-429's acceptance: a project-level common
// value lands in each installed tool's project file of the clone (the
// personal one by default, git-excluded when created), is recorded on the
// project layer, and the precedence view shows it at the project rank.
func TestCommonFanOutAtProject(t *testing.T) {
	h := newHarness(t, nil)
	h.write(".claude/settings.json", "{\"model\": \"sonnet\"}\n")
	if err := os.MkdirAll(filepath.Join(h.project, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	req := CommonRequest{Key: agentconfig.KeyModel, Value: "anthropic/claude-opus-4-1", Project: h.project}
	pv, err := h.svc.CommonPreview(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !pv.OK || pv.Writes < 1 {
		t.Fatalf("project common preview: %+v", pv)
	}
	req.PreviewEtag = pv.PreviewEtag
	out, err := h.svc.CommonSave(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(h.project, ".claude", "settings.local.json")
	if !out.Saved || !strings.Contains(h.read(local), "opus") {
		t.Fatalf("outcome %+v; local file missing the value", out)
	}
	if strings.Contains(h.read(filepath.Join(h.home, ".claude", "settings.json")), "opus") {
		t.Fatal("a project-level value was written into the user file")
	}
	if ex := h.read(filepath.Join(h.project, ".git", "info", "exclude")); !strings.Contains(ex, "settings.local.json") {
		t.Fatalf("exclude:\n%s", ex)
	}
	for _, w := range h.st.writes {
		if w.CommonKey == "model" && w.Layer != "l3_project" {
			t.Fatalf("project common write recorded on layer %s", w.Layer)
		}
	}
	view, err := h.svc.Inspect(ctx, "claude-code", h.project)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range view.Settings {
		if s.ID == "claude-code/model" {
			found = true
			if s.Effective.Layer != agentconfig.ValueL3 {
				t.Fatalf("effective layer %s, want the project rank: %+v", s.Effective.Layer, s.Effective)
			}
		}
	}
	if !found {
		t.Fatal("claude-code/model missing from the view")
	}
	// A bad placement is refused, never guessed.
	if _, err := h.svc.CommonPreview(ctx, CommonRequest{Key: agentconfig.KeyModel, Value: "anthropic/claude-opus-4-1", Project: h.project, Placement: "elsewhere"}); err == nil {
		t.Fatal("an unknown placement was accepted")
	}
}
