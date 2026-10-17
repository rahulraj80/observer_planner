package agentconfig

import (
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/cfgfmt"
)

func fileRow(t *testing.T, id string) FileRow {
	t.Helper()
	for _, f := range Files() {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("no file row %q", id)
	return FileRow{}
}

func TestResolvePath(t *testing.T) {
	user := fileRow(t, "claude-code/user-settings")
	proj := fileRow(t, "claude-code/project-settings")
	managed := fileRow(t, "claude-code/managed-settings")
	cases := []struct {
		name   string
		row    FileRow
		env    PathEnv
		status PathStatus
		path   string
	}{
		{"linux home", user, PathEnv{GOOS: "linux", Home: "/home/u"}, PathResolved, "/home/u/.claude/settings.json"},
		{"windows home", user, PathEnv{GOOS: "windows", Home: `C:\Users\u`}, PathResolved, `C:\Users\u\.claude\settings.json`},
		{"project missing", proj, PathEnv{GOOS: "linux", Home: "/home/u"}, PathNeedsProject, ""},
		{"project", proj, PathEnv{GOOS: "linux", Home: "/home/u", Project: "/src/app"}, PathResolved, "/src/app/.claude/settings.json"},
		{"system rerooted", managed, PathEnv{GOOS: "linux", Home: "/home/u", SystemRoot: "/tmp/sys"}, PathResolved, "/tmp/sys/etc/claude-code/managed-settings.json"},
		{"windows system rerooted", managed, PathEnv{GOOS: "windows", Home: `C:\Users\u`, SystemRoot: `D:\sys`}, PathResolved, `D:\sys\Program Files\ClaudeCode\managed-settings.json`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolvePath(c.row, c.env)
			if got.Status != c.status || got.Path != c.path {
				t.Fatalf("ResolvePath = %+v, want %s %q", got, c.status, c.path)
			}
		})
	}
	// %APPDATA% falls back to the home-relative default and honours the env.
	appdata := FileRow{Paths: OSPaths{Windows: `%APPDATA%\Code\User\settings.json`}}
	if got := ResolvePath(appdata, PathEnv{GOOS: "windows", Home: `C:\Users\u`}); got.Path != `C:\Users\u\AppData\Roaming\Code\User\settings.json` {
		t.Fatalf("APPDATA default = %q", got.Path)
	}
	env := func(k string) string {
		if k == "APPDATA" {
			return `E:\roam`
		}
		return ""
	}
	if got := ResolvePath(appdata, PathEnv{GOOS: "windows", Home: `C:\Users\u`, Getenv: env}); got.Path != `E:\roam\Code\User\settings.json` {
		t.Fatalf("APPDATA env = %q", got.Path)
	}
	glob := FileRow{Paths: samePath("<project>/.claude/skills/*/SKILL.md")}
	if got := ResolvePath(glob, PathEnv{GOOS: "linux", Home: "/h", Project: "/p"}); got.Status != PathGlob {
		t.Fatalf("glob status = %s", got.Status)
	}
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pat, rel string
		want     bool
	}{
		{"CLAUDE.md", "CLAUDE.md", true},
		{".claude/skills/*/SKILL.md", ".claude/skills/review/SKILL.md", true},
		{".claude/skills/*/SKILL.md", ".claude/skills/a/b/SKILL.md", false},
		{"**/AGENTS.md", "AGENTS.md", true},
		{"**/AGENTS.md", "pkg/sub/AGENTS.md", true},
		{".claude/commands/**/*.md", ".claude/commands/x.md", true},
		{".claude/commands/**/*.md", ".claude/commands/a/b/x.md", true},
		{".cursor/rules/**/*.mdc", ".cursor/rules/x.md", false},
		{"?.md", "ab.md", false},
	}
	for _, c := range cases {
		if got := MatchGlob(c.pat, c.rel); got != c.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", c.pat, c.rel, got, c.want)
		}
	}
}

func TestSecretKeyName(t *testing.T) {
	cases := map[string]bool{
		"apiKey": true, "OPENAI_API_KEY": true, "token": true, "password": true, "client_secret": true,
		"maxTokens": false, "apiKeyHelper": false, "envKey": false, "model": false, "tokenUrl": false,
		"CLAUDE_CODE_MAX_OUTPUT_TOKENS": false,
	}
	for k, want := range cases {
		if got := SecretKeyName(k); got != want {
			t.Errorf("SecretKeyName(%q) = %v, want %v", k, got, want)
		}
	}
}

func fakeDetect(s string) bool { return strings.HasPrefix(s, "sk-") }

func TestMaskAndRestore(t *testing.T) {
	src := []byte(`{
  // comment survives
  "model": "opus",
  "env": {"OPENAI_API_KEY": "abc123", "PATH_HINT": "x"},
  "mcp": {"x": {"args": ["--key", "sk-livekey"]}}
}`)
	md, err := maskDocument("jsonc", "test/shape", src, fakeDetect, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := string(md.Bytes)
	if strings.Contains(out, "abc123") || strings.Contains(out, "sk-livekey") {
		t.Fatalf("masked output leaks a secret:\n%s", out)
	}
	if !strings.Contains(out, "// comment survives") || !strings.Contains(out, `"PATH_HINT": "x"`) {
		t.Fatalf("masking lost non-secret content:\n%s", out)
	}
	// A raw save that sends the masked text back keeps the secrets.
	doc, err := parseJSONC(md.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := restoreSecrets(md, doc); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := string(doc.Bytes()); !strings.Contains(got, "abc123") || !strings.Contains(got, "sk-livekey") {
		t.Fatalf("restore did not put the secrets back:\n%s", got)
	}
	// Moving the placeholder is refused.
	moved := strings.Replace(string(md.Bytes), `"model": "opus"`, `"model": "`+SecretPlaceholder+`"`, 1)
	doc2, err := parseJSONC([]byte(moved))
	if err != nil {
		t.Fatal(err)
	}
	if err := restoreSecrets(md, doc2); err == nil {
		t.Fatal("a moved placeholder was accepted")
	}
}

const claudeUser = `{
  "model": "sonnet",
  "env": {
    "ANTHROPIC_BASE_URL": "http://127.0.0.1:8820"
  },
  "hooks": {
    "PreToolUse": [
      {"matcher": "*", "hooks": [{"type": "command", "command": "/usr/local/bin/observer hook claude-code pre-tool"}]},
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "my-lint.sh"}]}
    ]
  }
}
`

func claudeSave(t *testing.T, edits []Edit, raw *string, mod func(*SaveInput)) SaveResult {
	t.Helper()
	in := SaveInput{
		File:   SaveFile{Row: fileRow(t, "claude-code/user-settings"), Path: "/h/.claude/settings.json", Present: true, Bytes: []byte(claudeUser)},
		Edits:  edits,
		Raw:    raw,
		Detect: fakeDetect,
		Spans:  SpanEnv{ProxyAddrs: []string{"127.0.0.1:8820"}},
		Access: Access{SecretsPosture: SecretsWarn},
	}
	if mod != nil {
		mod(&in)
	}
	return PlanSave(in)
}

func codes(r SaveResult) map[string]Severity {
	out := map[string]Severity{}
	for _, f := range r.Findings {
		out[f.Code] = f.Severity
	}
	return out
}

func TestPlanSave(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := []struct {
		name      string
		edits     []Edit
		raw       *string
		mod       func(*SaveInput)
		ok        bool
		confirm   bool
		wantCodes []string
	}{
		{name: "set model", edits: []Edit{{Path: KeyPath{"model"}, Op: OpSet, Value: "opus"}}, ok: true},
		{name: "bad enum", edits: []Edit{{Path: KeyPath{"permissions", "defaultMode"}, Op: OpSet, Value: "yolo"}}, wantCodes: []string{"invalid_value"}},
		{name: "danger confirm", edits: []Edit{{Path: KeyPath{"permissions", "defaultMode"}, Op: OpSet, Value: "bypassPermissions"}}, ok: true, confirm: true, wantCodes: []string{"dangerous_value"}},
		{
			name: "disable hooks while integrated", edits: []Edit{{Path: KeyPath{"disableAllHooks"}, Op: OpSet, Value: true}},
			mod: func(in *SaveInput) { in.Integrated = true }, wantCodes: []string{"refused_while_integrated"},
		},
		{name: "disable hooks not integrated", edits: []Edit{{Path: KeyPath{"disableAllHooks"}, Op: OpSet, Value: true}}, ok: true},
		{
			name: "observer hook removed", raw: str(strings.Replace(claudeUser,
				`{"matcher": "*", "hooks": [{"type": "command", "command": "/usr/local/bin/observer hook claude-code pre-tool"}]},`, "", 1)),
			wantCodes: []string{"observer_span"},
		},
		{name: "user hook removed", raw: str(strings.Replace(claudeUser,
			`,
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "my-lint.sh"}]}`, "", 1)), ok: true},
		{
			name: "proxy route moved", edits: []Edit{{Path: KeyPath{"env", "ANTHROPIC_BASE_URL"}, Op: OpSet, Value: "https://api.anthropic.com"}},
			wantCodes: []string{"observer_span"},
		},
		{name: "uncatalogued key", edits: []Edit{{Path: KeyPath{"zzNotAKey"}, Op: OpSet, Value: 1}}, ok: true, wantCodes: []string{"uncatalogued_key"}},
		{name: "parse error", raw: str("{\n  \"model\": \n"), wantCodes: []string{"parse_error"}},
		{name: "plaintext secret warns", edits: []Edit{{Path: KeyPath{"env", "OPENAI_API_KEY"}, Op: OpSet, Value: "sk-abcdef"}}, ok: true, wantCodes: []string{"plaintext_secret"}},
		{
			name: "plaintext secret refused", edits: []Edit{{Path: KeyPath{"env", "OPENAI_API_KEY"}, Op: OpSet, Value: "sk-abcdef"}},
			mod: func(in *SaveInput) { in.Access.SecretsPosture = SecretsRefuse }, wantCodes: []string{"plaintext_secret"},
		},
		{
			name: "read only node", edits: []Edit{{Path: KeyPath{"model"}, Op: OpSet, Value: "opus"}},
			mod: func(in *SaveInput) { in.Access = Access{ReadOnly: true, Reason: "managed"} }, wantCodes: []string{"not_editable"},
		},
		{
			name: "org lock", edits: []Edit{{Path: KeyPath{"model"}, Op: OpSet, Value: "opus"}},
			mod: func(in *SaveInput) { in.Access.Locked = map[string]string{"claude-code/model": "Baseline"} }, wantCodes: []string{"org_locked"},
		},
		{name: "placeholder in form edit", edits: []Edit{{Path: KeyPath{"model"}, Op: OpSet, Value: SecretPlaceholder}}, wantCodes: []string{"secret_placeholder"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := claudeSave(t, c.edits, c.raw, c.mod)
			if r.OK != c.ok {
				t.Fatalf("OK = %v, want %v; findings %+v", r.OK, c.ok, r.Findings)
			}
			if r.NeedsConfirm != c.confirm {
				t.Fatalf("NeedsConfirm = %v, want %v", r.NeedsConfirm, c.confirm)
			}
			got := codes(r)
			for _, w := range c.wantCodes {
				if _, ok := got[w]; !ok {
					t.Fatalf("missing finding %q in %+v", w, r.Findings)
				}
			}
			if strings.Contains(r.Diff, "sk-abcdef") {
				t.Fatalf("diff echoes a secret:\n%s", r.Diff)
			}
		})
	}
}

func TestPlanSaveDiffAndEtags(t *testing.T) {
	r := claudeSave(t, []Edit{{Path: KeyPath{"model"}, Op: OpSet, Value: "opus"}}, nil, nil)
	if !strings.Contains(r.Diff, `-  "model": "sonnet",`) || !strings.Contains(r.Diff, `+  "model": "opus",`) {
		t.Fatalf("diff:\n%s", r.Diff)
	}
	if r.DiffStats.Added != 1 || r.DiffStats.Removed != 1 {
		t.Fatalf("stats %+v", r.DiffStats)
	}
	if r.BeforeEtag == "" || r.AfterEtag == "" || r.PreviewEtag != PreviewEtag(r.BeforeEtag, r.AfterEtag) {
		t.Fatalf("etags %q %q %q", r.BeforeEtag, r.AfterEtag, r.PreviewEtag)
	}
	if len(r.Changes) != 1 || r.Changes[0].SettingID != "claude-code/model" {
		t.Fatalf("changes %+v", r.Changes)
	}
	// Same value: no change.
	r = claudeSave(t, []Edit{{Path: KeyPath{"model"}, Op: OpSet, Value: "sonnet"}}, nil, nil)
	if !r.NoChange || r.Diff != "" {
		t.Fatalf("no-op save: NoChange=%v diff=%q", r.NoChange, r.Diff)
	}
}

func TestPlanSaveTOMLKeepsComments(t *testing.T) {
	src := "# my codex config\nmodel = \"gpt-5\" # trailing\n\n[tui]\nnotifications = true\n"
	in := SaveInput{
		File:   SaveFile{Row: fileRow(t, "codex/user-config"), Path: "/h/.codex/config.toml", Present: true, Bytes: []byte(src)},
		Edits:  []Edit{{Path: KeyPath{"model"}, Op: OpSet, Value: "gpt-5-codex"}},
		Access: Access{SecretsPosture: SecretsWarn},
	}
	r := PlanSave(in)
	if !r.OK {
		t.Fatalf("findings %+v", r.Findings)
	}
	if !strings.Contains(string(r.After), "# my codex config") || !strings.Contains(string(r.After), `model = "gpt-5-codex"`) {
		t.Fatalf("after:\n%s", r.After)
	}
}

func TestPlanSaveSkillFrontmatter(t *testing.T) {
	var row FileRow
	for _, f := range Files() {
		if f.Kind == KindSkill && f.Tool == "claude-code" && f.Scope == ScopeProjectShared {
			row = f
		}
	}
	if row.ID == "" {
		t.Fatal("no claude-code project skill row")
	}
	bad := "---\ndescription: does things\n---\nbody\n"
	r := PlanSave(SaveInput{File: SaveFile{Row: row, Path: "/p/.claude/skills/x/SKILL.md"}, Raw: &bad, Access: Access{SecretsPosture: SecretsWarn}})
	if r.OK || codes(r)["frontmatter_required"] != SevError {
		t.Fatalf("missing name accepted: %+v", r.Findings)
	}
	good := "---\nname: x\ndescription: does things\n---\nbody\n"
	r = PlanSave(SaveInput{File: SaveFile{Row: row, Path: "/p/.claude/skills/x/SKILL.md"}, Raw: &good, Access: Access{SecretsPosture: SecretsWarn}})
	if !r.OK || !r.Created {
		t.Fatalf("good skill: %+v", r.Findings)
	}
}

func TestInspectTool(t *testing.T) {
	user := fileRow(t, "claude-code/user-settings")
	proj := fileRow(t, "claude-code/project-settings")
	local := fileRow(t, "claude-code/project-local-settings")
	files := []FileSnapshot{
		{
			Row: user, Resolved: ResolvedPath{Status: PathResolved, Path: "/h/.claude/settings.json"}, State: FilePresent,
			Bytes: []byte(`{"model":"sonnet","env":{"OPENAI_API_KEY":"abc"},"zzUnknown":1}`),
		},
		{
			Row: proj, Resolved: ResolvedPath{Status: PathResolved, Path: "/p/.claude/settings.json"}, State: FilePresent,
			Bytes: []byte(`{"model":"opus"}`),
		},
		{Row: local, Resolved: ResolvedPath{Status: PathResolved, Path: "/p/.claude/settings.local.json"}, State: FileMissing},
	}
	v := InspectTool(InspectInput{
		Tool: "claude-code", Files: files, Detect: fakeDetect, Access: Access{SecretsPosture: SecretsWarn},
		Layers: []LayerRecord{{FilePath: "/h/.claude/settings.json", Key: "model", Layer: ValueL1, ValueHash: ValueHash("sonnet")}},
	})
	byID := map[string]SettingView{}
	for _, s := range v.Settings {
		byID[s.ID] = s
	}
	m := byID["claude-code/model"]
	if m.Effective.Value != "opus" || m.Effective.FileID != proj.ID || m.Effective.Layer != ValueL3 {
		t.Fatalf("model effective = %+v", m.Effective)
	}
	if len(m.Values) != 2 || m.Values[1].Layer != ValueL1 || len(m.Effective.Shadowed) != 1 {
		t.Fatalf("model values = %+v", m.Values)
	}
	var uf FileView
	for _, f := range v.Files {
		if f.ID == user.ID {
			uf = f
		}
	}
	if len(uf.Secrets) != 1 || uf.Secrets[0] != "env.OPENAI_API_KEY" {
		t.Fatalf("secrets = %v", uf.Secrets)
	}
	found := false
	for _, k := range uf.Uncatalogued {
		if k == "zzUnknown" {
			found = true
		}
	}
	if !found {
		t.Fatalf("uncatalogued = %v", uf.Uncatalogued)
	}
	if env := byID["claude-code/env"]; env.ID != "" {
		for _, val := range env.Values {
			if m, ok := val.Value.(map[string]any); ok && m["OPENAI_API_KEY"] == "abc" {
				t.Fatalf("env value leaks the key: %+v", val)
			}
		}
	}
}

func TestPlanFanOut(t *testing.T) {
	installed := func(tool string) bool { return tool == "claude-code" || tool == "codex" }
	targets, err := PlanFanOut(KeyTheme, "dark", false, installed)
	if err != nil {
		t.Fatal(err)
	}
	var writes, skipped int
	for _, tg := range targets {
		switch tg.Status {
		case FanOutWrite:
			writes++
			if tg.FileID == "" || len(tg.Edits) == 0 {
				t.Fatalf("write target without a file or edits: %+v", tg)
			}
		case FanOutNotInstalled:
			skipped++
		}
	}
	if writes == 0 || skipped == 0 {
		t.Fatalf("writes=%d skipped=%d targets=%+v", writes, skipped, targets)
	}
	if _, err := PlanFanOut(KeyHooks, map[string]any{}, false, installed); err == nil {
		t.Fatal("an Observer-owned key was accepted")
	}
	if _, err := PlanFanOut(KeyTheme, "purple", false, installed); err == nil {
		t.Fatal("a non-canonical theme was accepted")
	}
}

func parseJSONC(b []byte) (cfgfmt.Doc, error) { return cfgfmt.Parse(cfgfmt.FormatJSONC, b) }

// TestCountNoun: counts read "1 file" / "2 files", never "file(s)".
func TestCountNoun(t *testing.T) {
	cases := []struct {
		n         int
		one, many string
		want      string
	}{
		{1, "plaintext credential value", "", "1 plaintext credential value"},
		{2, "plaintext credential value", "", "2 plaintext credential values"},
		{0, "file", "", "0 files"},
		{3, "entry", "entries", "3 entries"},
	}
	for _, tc := range cases {
		if got := CountNoun(tc.n, tc.one, tc.many); got != tc.want {
			t.Errorf("CountNoun(%d, %q, %q) = %q, want %q", tc.n, tc.one, tc.many, got, tc.want)
		}
	}
}

// TestPlanFanOutAtProject is the T-429 scope table (D-106: all adapters ->
// per adapter -> project): the same common value lands in each tool's
// project file of the chosen placement, never in a user file, and a tool
// with no file of that placement says so instead of falling back.
func TestPlanFanOutAtProject(t *testing.T) {
	installed := func(tool string) bool { return tool == "claude-code" || tool == "codex" }
	cases := []struct {
		name      string
		scope     FanOutScope
		wantClaud string
		wantCodex []FanOutStatus
	}{
		{"user scope keeps the L1 user files", FanOutScope{}, "claude-code/user-settings", []FanOutStatus{FanOutWrite, FanOutRefused}},
		{"project, personal file", FanOutScope{Project: true, LocalOnly: true}, "claude-code/project-local-settings", []FanOutStatus{FanOutNoProjectFile, FanOutRefused}},
		{"project, shared file", FanOutScope{Project: true}, "claude-code/project-settings", []FanOutStatus{FanOutWrite, FanOutRefused}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			targets, err := PlanFanOutAt(KeyModel, "anthropic/claude-sonnet-4-6", false, installed, c.scope)
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			for _, tg := range targets {
				seen[tg.Tool] = true
				switch tg.Tool {
				case "claude-code":
					if tg.Status != FanOutWrite || tg.FileID != c.wantClaud {
						t.Fatalf("claude-code target %+v, want write to %s", tg, c.wantClaud)
					}
				case "codex":
					ok := false
					for _, s := range c.wantCodex {
						ok = ok || tg.Status == s
					}
					if !ok {
						t.Fatalf("codex target %+v, want one of %v", tg, c.wantCodex)
					}
					if tg.Status == FanOutWrite && c.scope.Project && tg.FileID != "codex/project-config" {
						t.Fatalf("codex project write went to %s", tg.FileID)
					}
				}
				if c.scope.Project && tg.Status == FanOutWrite {
					if r, ok := fileRowByID(tg.FileID); !ok || r.Scope == ScopeUser {
						t.Fatalf("a project-level value targets a user file: %+v", tg)
					}
				}
			}
			if !seen["claude-code"] {
				t.Fatalf("claude-code missing: %+v", targets)
			}
		})
	}
}

// TestOrgBlockLock pins the D-107 lock (T-426): only the organization's own
// apply writes or removes an org block, and a developer save that adds,
// edits or removes one is refused while the node holds org instructions;
// the text around the block stays editable.
func TestOrgBlockLock(t *testing.T) {
	row := FileRow{ID: "claude-code/instructions:CLAUDE.md", Tool: "claude-code", Kind: KindInstructions, Scope: ScopeProjectShared, Format: FormatMarkdown, WriteNeeds: PrivUser}
	file := "# Mine\n\n<!-- observer:org-begin rev=1 -->\nOrg rule.\n<!-- observer:org-end -->\n"
	locked := Access{SecretsPosture: SecretsWarn, OrgBlockLocked: "Baseline"}
	raw := func(s string) *string { return &s }
	cases := []struct {
		name   string
		access Access
		raw    *string
		edits  []Edit
		ok     bool
	}{
		{"developer edits outside the block", locked, raw("# Mine, edited\n\n<!-- observer:org-begin rev=1 -->\nOrg rule.\n<!-- observer:org-end -->\n"), nil, true},
		{"developer edits inside the block", locked, raw("# Mine\n\n<!-- observer:org-begin rev=1 -->\nNo rule.\n<!-- observer:org-end -->\n"), nil, false},
		{"developer removes the block", locked, raw("# Mine\n"), nil, false},
		{"no org instructions held: free", Access{SecretsPosture: SecretsWarn}, raw("# Mine\n"), nil, true},
		{"developer may not send the org op", Access{SecretsPosture: SecretsWarn}, nil, []Edit{{Path: KeyPath{"(org block)"}, Op: OpOrgBlock, Value: "x"}}, false},
		{"the org apply writes the block", Access{SecretsPosture: SecretsWarn, OrgWrite: true, OrgBlockLocked: "Baseline"}, nil, []Edit{{Path: KeyPath{"(org block)"}, Op: OpOrgBlock, Value: "New rule."}}, true},
		{"the org apply removes the block", Access{SecretsPosture: SecretsWarn, OrgWrite: true}, nil, []Edit{{Path: KeyPath{"(org block)"}, Op: OpRemoveOrgBlock}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := PlanSave(SaveInput{
				File: SaveFile{Row: row, Path: "/p/CLAUDE.md", Present: true, Bytes: []byte(file)},
				Raw:  c.raw, Edits: c.edits, Access: c.access,
			})
			if res.OK != c.ok {
				t.Fatalf("ok = %v, want %v: %+v", res.OK, c.ok, res.Findings)
			}
			if c.ok && len(c.edits) > 0 && c.edits[0].Op == OpRemoveOrgBlock && string(res.After) != "# Mine\n" {
				t.Fatalf("removal left %q", res.After)
			}
			if c.ok && len(c.edits) > 0 && c.edits[0].Op == OpOrgBlock && !strings.Contains(string(res.After), "New rule.") {
				t.Fatalf("block not written: %q", res.After)
			}
		})
	}
}
