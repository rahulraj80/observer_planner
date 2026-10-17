package gemini

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// tokenEventsFor builds token events from (id, output) pairs in one file.
func tokenEventsFor(pairs ...any) []models.TokenEvent {
	var out []models.TokenEvent
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, models.TokenEvent{SourceFile: "f", SourceEventID: pairs[i].(string), OutputTokens: int64(pairs[i+1].(int))})
	}
	return out
}

// TestOneTokenEventPerCall pins T-543 on live captures: the Gemini CLI
// appends an assistant message twice (text first, again once its
// toolCalls resolve), and each model call must still come out of a parse
// exactly once, carrying the restatement's counts.
func TestOneTokenEventPerCall(t *testing.T) {
	for _, tc := range []struct {
		name, rel, fixture string
		wantCalls          int
	}{
		{"live tool calls (gemini-cli 2026-06)", "h1/chats/session-live-toolcalls.jsonl", filepath.Join("..", "..", "..", "testdata", "gemini", "session-live-toolcalls.jsonl"), 3},
		{"live OTLP capture (gemini-cli 0.53.1)", "h2/chats/session-2026-10-05T23-24-655fdd60.jsonl", filepath.Join("..", "..", "..", "testdata", "otlp", "gemini-cli", "session-2026-10-05T23-24-655fdd60.jsonl"), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFixture(t, tc.rel, tc.fixture)
			res, err := New().ParseSessionFile(context.Background(), path, 0)
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]int{}
			for _, e := range res.TokenEvents {
				seen[e.SourceEventID]++
			}
			if len(res.TokenEvents) != tc.wantCalls || len(seen) != tc.wantCalls {
				t.Fatalf("token events = %d (distinct ids %d), want one per call = %d: %v", len(res.TokenEvents), len(seen), tc.wantCalls, seen)
			}
		})
	}
}

// TestCollapseKeepsFirstPositionLastValues pins the collapse rule itself.
func TestCollapseKeepsFirstPositionLastValues(t *testing.T) {
	in := tokenEventsFor("a", 1, "b", 5, "a", 2, "", 7, "", 8)
	out := collapseTokenRestatements(in)
	if len(out) != 4 {
		t.Fatalf("len = %d, want 4 (a, b, and the two id-less rows kept apart)", len(out))
	}
	if out[0].SourceEventID != "a" || out[0].OutputTokens != 2 || out[1].SourceEventID != "b" {
		t.Fatalf("collapse = %+v, want a (last values) then b", out)
	}
}
