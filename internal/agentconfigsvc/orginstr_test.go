package agentconfigsvc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/agentconfigorg"
	"github.com/marmutapp/superbased-observer/internal/orgcontract"
	"github.com/marmutapp/superbased-observer/internal/store"
)

func instrRow(t *testing.T, rows []store.AgentConfigOrgStateRow, tool, fileID, root string) store.AgentConfigOrgStateRow {
	t.Helper()
	for _, r := range rows {
		if r.SettingID == orgcontract.AgentConfigInstructionSetting(tool) && r.FileID == fileID && r.ProjectRoot == root {
			return r
		}
	}
	t.Fatalf("no instruction row for %s %s %q in %+v", tool, fileID, root, rows)
	return store.AgentConfigOrgStateRow{}
}

// TestOrgInstructionsEndToEnd is T-426's acceptance on a managed node: a
// published org instruction lands as a marked block in each installed tool's
// own instruction file at the user level and in the project's clone, ADDED
// to what the developer wrote; a hand edit inside the block is put back; a
// withdrawal removes every block and leaves the developer's text byte for
// byte.
func TestOrgInstructionsEndToEnd(t *testing.T) {
	committed := false
	h := newHarness(t, func(o *Options) {
		o.Origin = "org"
		o.Committed = func(string, string) bool { return committed }
	})
	h.write(".claude/settings.json", "{}\n")
	h.write(".codex/config.toml", "model = \"o3\"\n")
	userClaude := h.write(".claude/CLAUDE.md", "# My notes\nKeep it short.\n")
	projAgents := h.write("<project>/AGENTS.md", "# Team agents\n")
	roots := map[string][]string{"abc123": {h.project}}
	body := orgcontract.AgentConfigPolicyBody{Instructions: []orgcontract.AgentConfigInstruction{
		{Text: "Use the internal package registry.", Policy: "Baseline", Layer: orgcontract.AgentConfigLayerOrg},
		{Project: "abc123", Placement: orgcontract.AgentConfigPlacementShared, Text: "Run make test before a commit.", Policy: "Apps", Layer: orgcontract.AgentConfigLayerTeam},
	}}
	ctx := context.Background()
	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	res := h.svc.ApplyOrg(ctx, OrgApplyInput{Body: body, Version: 5, ProjectRoots: roots, Now: now})

	got := h.read(userClaude)
	if !strings.HasPrefix(got, "# My notes\nKeep it short.\n") || !strings.Contains(got, "<!-- observer:org-begin rev=") || !strings.Contains(got, "Use the internal package registry.\n<!-- observer:org-end -->") {
		t.Fatalf("user CLAUDE.md:\n%s", got)
	}
	if got := h.read(filepath.Join(h.home, ".codex", "AGENTS.md")); !strings.Contains(got, "Use the internal package registry.") {
		t.Fatalf("user codex AGENTS.md:\n%s", got)
	}
	if got := h.read(projAgents); !strings.HasPrefix(got, "# Team agents\n") || !strings.Contains(got, "Run make test before a commit.") {
		t.Fatalf("project AGENTS.md:\n%s", got)
	}
	if got := h.read(filepath.Join(h.project, "CLAUDE.md")); !strings.Contains(got, "Run make test before a commit.") {
		t.Fatalf("project CLAUDE.md:\n%s", got)
	}
	st := instrRow(t, res.Rows, "claude-code", "claude-code/instructions:~/.claude/CLAUDE.md", "")
	if st.State != string(agentconfigorg.StateApplied) || st.PolicyName != "Baseline" || st.Mode != "enforced" {
		t.Fatalf("user claude row %+v", st)
	}
	pst := instrRow(t, res.Rows, "codex", "codex/instructions:AGENTS.md", h.project)
	if pst.State != string(agentconfigorg.StateApplied) || pst.Scope != "project_shared" || pst.ProjectHash != "abc123" {
		t.Fatalf("project codex row %+v", pst)
	}
	if res.Writes != 4 {
		t.Fatalf("writes = %d, want 4 (two user files, two project files)", res.Writes)
	}

	// An unchanged pass writes nothing.
	again := h.svc.ApplyOrg(ctx, OrgApplyInput{Body: body, Version: 5, Prior: res.Rows, ProjectRoots: roots, Now: now.Add(time.Minute)})
	if again.Writes != 0 {
		t.Fatalf("an unchanged pass wrote %d files", again.Writes)
	}

	// A hand edit inside the block is drift and is put back; the
	// developer's own text is untouched.
	h.write(".claude/CLAUDE.md", strings.Replace(h.read(userClaude), "Use the internal package registry.", "Use any registry.", 1))
	committed = true
	third := h.svc.ApplyOrg(ctx, OrgApplyInput{Body: body, Version: 5, Prior: again.Rows, ProjectRoots: roots, Now: now.Add(2 * time.Minute)})
	if st := instrRow(t, third.Rows, "claude-code", "claude-code/instructions:~/.claude/CLAUDE.md", ""); st.State != string(agentconfigorg.StateReasserted) || third.DriftEvents != 1 {
		t.Fatalf("hand edit: %+v drift=%d", st, third.DriftEvents)
	}
	if got := h.read(userClaude); !strings.Contains(got, "Use the internal package registry.") || strings.Contains(got, "Use any registry.") {
		t.Fatalf("not put back:\n%s", got)
	}
	// A tracked file whose HEAD already carries the block is reported.
	if r := instrRow(t, third.Rows, "codex", "codex/instructions:AGENTS.md", h.project); r.Reason != "org_block_committed" {
		t.Fatalf("committed block not reported: %+v", r)
	}

	// Withdrawal: a signed empty body removes every block it wrote.
	gone := h.svc.ApplyOrg(ctx, OrgApplyInput{Body: orgcontract.AgentConfigPolicyBody{}, Version: 6, Prior: third.Rows, ProjectRoots: roots, Now: now.Add(3 * time.Minute)})
	if len(gone.Rows) != 0 {
		t.Fatalf("withdrawal left rows: %+v", gone.Rows)
	}
	if got := h.read(userClaude); got != "# My notes\nKeep it short.\n" {
		t.Fatalf("user CLAUDE.md after withdrawal:\n%q", got)
	}
	if got := h.read(projAgents); got != "# Team agents\n" {
		t.Fatalf("project AGENTS.md after withdrawal:\n%q", got)
	}
	for _, p := range []string{filepath.Join(h.home, ".codex", "AGENTS.md"), filepath.Join(h.project, "CLAUDE.md")} {
		if strings.Contains(h.read(p), "observer:org") {
			t.Fatalf("%s kept the block", p)
		}
	}
}

// TestOrgInstructionsLocalOnly: a local-only project instruction lands in the
// tool's personal instruction file (git-excluded), and a tool that has no
// personal instruction file is reported, never written into a tracked file.
func TestOrgInstructionsLocalOnly(t *testing.T) {
	h := newHarness(t, nil)
	h.write(".claude/settings.json", "{}\n")
	h.write(".codex/config.toml", "model = \"o3\"\n")
	if err := os.MkdirAll(filepath.Join(h.project, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := orgcontract.AgentConfigPolicyBody{Instructions: []orgcontract.AgentConfigInstruction{
		{Project: "abc123", Placement: orgcontract.AgentConfigPlacementLocalOnly, Text: "Personal reminder.", Policy: "Dana", Layer: orgcontract.AgentConfigLayerMember},
	}}
	res := h.svc.ApplyOrg(context.Background(), OrgApplyInput{Body: body, Version: 2, ProjectRoots: map[string][]string{"abc123": {h.project}}, Now: time.Now()})
	if got := h.read(filepath.Join(h.project, "CLAUDE.local.md")); !strings.Contains(got, "Personal reminder.") {
		t.Fatalf("CLAUDE.local.md:\n%s", got)
	}
	if ex := h.read(filepath.Join(h.project, ".git", "info", "exclude")); !strings.Contains(ex, "CLAUDE.local.md") {
		t.Fatalf("exclude:\n%s", ex)
	}
	if _, err := os.Stat(filepath.Join(h.project, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("a local-only instruction created a tracked file")
	}
	r := instrRow(t, res.Rows, "codex", OrgFileNone, h.project)
	if r.State != string(agentconfigorg.StateNoFile) || r.Reason != "no_instruction_file" {
		t.Fatalf("codex row %+v", r)
	}
	if p := instrRow(t, res.Rows, "claude-code", "claude-code/instructions:CLAUDE.local.md", h.project); !p.Personal || p.State != string(agentconfigorg.StateApplied) {
		t.Fatalf("claude row %+v", p)
	}
}
