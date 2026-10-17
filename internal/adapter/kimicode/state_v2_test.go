package kimicode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// TestParseStateV2Cwd pins the version-2 state.json (demo node 1,
// 2026-10-02): the session cwd is `cwd`, createdAt is a number. Before
// the fix the numeric createdAt failed the decode, every event had an
// empty project root, and the store dropped the whole session (so no
// prompt reached the guard's capture lane either).
func TestParseStateV2Cwd(t *testing.T) {
	wire, err := os.ReadFile(mainWire)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "sessions")
	sess := filepath.Join(root, "wd_spoon-knife_dac98f0dd824", "session_afc78733-440d-451a-affc-335e475c676f")
	agent := filepath.Join(sess, "agents", "main")
	if err := os.MkdirAll(agent, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(agent, "wire.jsonl")
	if err := os.WriteFile(path, wire, 0o600); err != nil {
		t.Fatal(err)
	}
	state := `{"id":"session_afc78733-440d-451a-affc-335e475c676f","version":2,"cwd":"/home/dev/work/Spoon-Knife","createdAt":1790960377184,"updatedAt":1790963350179,"archived":false,"agents":{"main":{"homedir":"x","type":"main"}},"custom":{},"title":"hi","titleKind":"auto"}`
	if err := os.WriteFile(filepath.Join(sess, "state.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	a := NewWithOptions(nil, root)
	res, err := a.ParseSessionFile(context.Background(), path, 0)
	if err != nil {
		t.Fatal(err)
	}
	prompt := toolByAction(res, models.ActionUserPrompt)
	if prompt == nil {
		t.Fatal("no user_prompt event")
	}
	if prompt.ProjectRoot != "/home/dev/work/Spoon-Knife" {
		t.Errorf("project root = %q, want the v2 cwd", prompt.ProjectRoot)
	}
}
