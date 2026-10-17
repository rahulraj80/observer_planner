package cursor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// TestRetryReplayFold pins T-034i. The fixture is the shape of node 1's
// cursor-agent transcript bb9f3afb (cursor-agent 2026.10.01, 2026-10-03,
// anonymized): a LostConnection retry appended the SAME reply to the
// transcript once per attempt, so one reply became four assistant lines.
// Each case is one turn; the table says how many assistant rows survive and
// the replay count on the first.
func TestRetryReplayFold(t *testing.T) {
	user := `{"role":"user","message":{"content":[{"type":"text","text":"<user_query>say OK</user_query>"}]}}`
	ok := `{"role":"assistant","message":{"content":[{"type":"text","text":"OK"}]}}`
	okSpaced := `{"role":"assistant","message":{"content":[{"type":"text","text":"  OK \n"}]}}`
	other := `{"role":"assistant","message":{"content":[{"type":"text","text":"Something else"}]}}`
	tool := `{"role":"assistant","message":{"content":[{"type":"tool_use","name":"Shell","input":{"command":"ls"}}]}}`
	mixed := `{"role":"assistant","message":{"content":[{"type":"text","text":"OK"},{"type":"tool_use","name":"Shell","input":{"command":"ls"}}]}}`
	for _, tc := range []struct {
		name        string
		lines       []string
		wantText    int
		wantReplays int
	}{
		{"four identical retries fold to one", []string{user, ok, ok, ok, ok}, 1, 3},
		{"whitespace-only difference still folds", []string{user, ok, okSpaced}, 1, 1},
		{"a single reply is untouched", []string{user, ok}, 1, 0},
		{"different text is kept", []string{user, ok, other}, 2, 0},
		{"a tool call in between keeps the repeat", []string{user, ok, tool, ok}, 2, 0},
		{"a mixed line never folds", []string{user, mixed, mixed}, 2, 0},
		{"a new user turn starts over", []string{user, ok, user, ok}, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "t.jsonl")
			if err := os.WriteFile(p, []byte(strings.Join(tc.lines, "\n")+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			turns, err := parseTranscriptTurns(p)
			if err != nil {
				t.Fatal(err)
			}
			last := turns[len(turns)-1]
			evs := buildTranscriptToolEvents(last, "conv", "/repo", "gen", p, time.Unix(0, 0), nil)
			var text []models.ToolEvent
			for _, ev := range evs {
				if ev.ActionType == models.ActionAssistantMessage {
					text = append(text, ev)
				}
			}
			if len(text) != tc.wantText {
				t.Fatalf("assistant rows = %d, want %d", len(text), tc.wantText)
			}
			got := 0
			if text[0].Metadata != nil {
				got = text[0].Metadata.RetryReplays
			}
			if got != tc.wantReplays {
				t.Fatalf("RetryReplays = %d, want %d", got, tc.wantReplays)
			}
			if tc.wantReplays == 0 && text[0].Metadata != nil && !text[0].Metadata.IsZero() {
				t.Fatalf("an unreplayed reply carries metadata %+v", text[0].Metadata)
			}
		})
	}
}
