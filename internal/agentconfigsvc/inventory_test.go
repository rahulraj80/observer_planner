package agentconfigsvc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/orgcontract"
)

func invRow(rows []orgcontract.AgentConfigStateRow, setting, fileID, project string) (orgcontract.AgentConfigStateRow, bool) {
	for _, r := range rows {
		if r.Setting == setting && r.FileID == fileID && r.ProjectHash == project {
			return r, true
		}
	}
	return orgcontract.AgentConfigStateRow{}, false
}

// TestInventory is T-427's node half (D-112, the org sees everything on a
// managed node): every catalogued setting found in an installed tool's files
// and every instruction-family file, user level and project clone, a
// personal file's value included, every secret scrubbed (never dropped,
// never raw), never-read files never opened, and no local path on the rows.
func TestInventory(t *testing.T) {
	h := newHarness(t, nil)
	const rawKey = "AKIAIOSFODNN7EXAMPLE"
	h.write(".claude/settings.json", `{"model": "sonnet", "env": {"ANTHROPIC_API_KEY": "plain-secret-value"}}`+"\n")
	h.write(".claude.json", `{"oauthAccount": {"token": "NEVER-READ-SENTINEL"}}`)
	h.write(".claude/CLAUDE.md", "# Notes\nSENTINEL-NOTE uses key "+rawKey+" for builds.\n")
	h.write("<project>/.claude/settings.local.json", `{"model": "opus"}`+"\n")
	h.write("<project>/.claude/skills/review/SKILL.md", "---\nname: review\ndescription: Review code\n---\nSENTINEL-SKILL\n")
	hash := strings.Repeat("ab", 32)
	roots := map[string][]string{hash: {h.project}, "not-a-hash": {h.project}}

	rep := h.svc.Inventory(context.Background(), roots)
	if rep.Digest == "" || rep.Truncated {
		t.Fatalf("report %+v", rep)
	}
	for _, r := range rep.Rows {
		if r.State != orgcontract.AgentConfigInventoryState || r.Mode != "" || r.Layer != "" {
			t.Fatalf("row not an inventory row: %+v", r)
		}
		if r.Tool != "claude-code" {
			t.Fatalf("row for a tool that is not installed: %+v", r)
		}
	}
	if r, ok := invRow(rep.Rows, "claude-code/model", "claude-code/user-settings", ""); !ok || r.ObservedValue != `"sonnet"` || r.Scope != "user" {
		t.Fatalf("user model row %+v (found %v)", r, ok)
	}
	if r, ok := invRow(rep.Rows, "claude-code/env.ANTHROPIC_API_KEY", "claude-code/user-settings", ""); !ok || r.ObservedValue != `"[REDACTED]"` || r.Reason != orgcontract.AgentConfigInventorySecret {
		t.Fatalf("secret row must ship scrubbed, not be dropped: %+v (found %v)", r, ok)
	}
	if r, ok := invRow(rep.Rows, "claude-code/model", "claude-code/project-local-settings", hash); !ok || r.ObservedValue != `"opus"` || r.Scope != "project_local" {
		t.Fatalf("personal project value must ship: %+v (found %v)", r, ok)
	}
	ins, ok := invRow(rep.Rows, "claude-code/@instructions", "claude-code/instructions:~/.claude/CLAUDE.md", "")
	if !ok || !strings.Contains(ins.ObservedValue, "SENTINEL-NOTE") || ins.Reason != orgcontract.AgentConfigInventorySecret {
		t.Fatalf("user CLAUDE.md row %+v (found %v)", ins, ok)
	}
	if sk, ok := invRow(rep.Rows, "claude-code/@skill", "claude-code/skill:.claude/skills/review/SKILL.md", hash); !ok || !strings.Contains(sk.ObservedValue, "SENTINEL-SKILL") {
		t.Fatalf("project skill row %+v (found %v); rows %+v", sk, ok, rep.Rows)
	}
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	for _, never := range []string{rawKey, "plain-secret-value", "NEVER-READ-SENTINEL", h.home, h.project, "not-a-hash"} {
		if strings.Contains(string(b), never) {
			t.Fatalf("the inventory carries %q: %s", never, b)
		}
	}
	if again := h.svc.Inventory(context.Background(), roots); again.Digest != rep.Digest {
		t.Fatal("an unchanged machine must yield the same digest (timestamps are not hashed)")
	}
}

// TestFinishInventoryBudget pins the caps: rows past the value budget keep
// their hash and lose the value, and a long text is cut with the truncated
// reason.
func TestFinishInventoryBudget(t *testing.T) {
	big, reason := encodeInventoryText(strings.Repeat("é", orgcontract.AgentConfigObservedMaxBytes), "")
	if len(big) > orgcontract.AgentConfigObservedMaxBytes || reason != orgcontract.AgentConfigInventoryTruncated || !json.Valid([]byte(big)) {
		t.Fatalf("cut text: %d bytes, reason %q", len(big), reason)
	}
	var rows []orgcontract.AgentConfigStateRow
	for i := 0; i < 200; i++ {
		rows = append(rows, orgcontract.AgentConfigStateRow{
			Tool: "codex", Setting: "codex/@instructions", FileID: "codex/instructions:f" + strings.Repeat("x", i),
			State: orgcontract.AgentConfigInventoryState, ObservedHash: "h", ObservedValue: big,
		})
	}
	rep := finishInventory(rows)
	total, cut := 0, 0
	for _, r := range rep.Rows {
		total += len(r.ObservedValue)
		if r.Reason == orgcontract.AgentConfigInventoryBudget {
			cut++
			if r.ObservedValue != "" || r.ObservedHash == "" {
				t.Fatalf("a budget row keeps its hash and drops its value: %+v", r)
			}
		}
	}
	if total > orgcontract.AgentConfigInventoryMaxBytes || cut == 0 {
		t.Fatalf("budget: %d bytes, %d rows cut", total, cut)
	}
}
