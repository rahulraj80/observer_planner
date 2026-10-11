package pi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// TestParseSessionFile_StringContentDecodes pins the demo node 1 shape
// (2026-10-02): a message whose content is a bare string (a system message
// there) failed the whole line. A user prompt written that way must still
// become a user_prompt, and an empty string must not warn.
func TestParseSessionFile_StringContentDecodes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session.jsonl")
	body := strings.Join([]string{
		`{"type":"session","version":3,"id":"ses_s","timestamp":"2026-10-02T18:29:28.483Z","cwd":"/home/dev/repo"}`,
		`{"type":"message","id":"s1","parentId":null,"timestamp":"2026-10-02T18:29:29.000Z","message":{"role":"system","content":""}}`,
		`{"type":"message","id":"u1","parentId":"s1","timestamp":"2026-10-02T18:29:30.000Z","message":{"role":"user","content":"push the release"}}`,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := NewWithOptions(nil, []string{root}).ParseSessionFile(context.Background(), path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", res.Warnings)
	}
	found := false
	for _, ev := range res.ToolEvents {
		if ev.ActionType == models.ActionUserPrompt && strings.Contains(ev.Target, "push the release") {
			found = true
		}
	}
	if !found {
		t.Fatalf("string-content user prompt not captured: %+v", res.ToolEvents)
	}
}
