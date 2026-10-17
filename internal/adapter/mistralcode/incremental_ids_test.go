package mistralcode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// TestParseSessionFile_IncrementalUserIDsAreFileGlobal pins the demo node 1
// bug (2026-10-02): vibe appends one message per write, so each incremental
// parse saw its new user prompt at slice index 0 and minted the SAME
// SourceEventID ("user:<sess>:0") as the first prompt; the store dropped
// it as a duplicate. Ids (and the synthesized timestamps) must be the
// record's index in the whole file, identical to a parse from offset 0.
func TestParseSessionFile_IncrementalUserIDsAreFileGlobal(t *testing.T) {
	root := t.TempDir()
	msgPath := writeVibeSession(t, root)
	a := NewWithOptions(nil, filepath.Join(root, ".vibe", "logs", "session"))

	first, err := a.ParseSessionFile(context.Background(), msgPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := os.ReadFile(msgPath)
	if err != nil {
		t.Fatal(err)
	}
	appended := `{"role":"user","content":"second prompt","message_id":"u2"}` + "\n"
	if err := os.WriteFile(msgPath, append(prior, appended...), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := a.ParseSessionFile(context.Background(), msgPath, first.NewOffset)
	if err != nil {
		t.Fatal(err)
	}
	full, err := a.ParseSessionFile(context.Background(), msgPath, 0)
	if err != nil {
		t.Fatal(err)
	}

	userIDs := func(evs []models.ToolEvent) map[string]string {
		out := map[string]string{}
		for _, e := range evs {
			if e.ActionType == models.ActionUserPrompt {
				out[e.Target] = e.SourceEventID
			}
		}
		return out
	}
	firstIDs, secondIDs, fullIDs := userIDs(first.ToolEvents), userIDs(second.ToolEvents), userIDs(full.ToolEvents)
	got, ok := secondIDs["second prompt"]
	if !ok {
		t.Fatalf("incremental parse emitted no prompt: %+v", second.ToolEvents)
	}
	for _, id := range firstIDs {
		if id == got {
			t.Fatalf("incremental prompt id %q collides with an earlier prompt", got)
		}
	}
	if fullIDs["second prompt"] != got {
		t.Errorf("incremental id %q != from-zero id %q", got, fullIDs["second prompt"])
	}
}
