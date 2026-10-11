package store

import (
	"context"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/models"
)

// TestT570PromptCountsFoldHookShadows pins T-570 (b): outside the session
// Messages tab, user_prompt counts (a project's per-session prompt counts,
// the predictor's message boundaries) counted a Codex hook prompt and its
// transcript twin as two prompts. They now go through the same prompt-shadow
// fold; a tool that does not declare the shadow keeps every row.
func TestT570PromptCountsFoldHookShadows(t *testing.T) {
	s, database := newTestStore(t)
	ctx := context.Background()
	pid, err := s.UpsertProject(ctx, "/t570/p", "")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		session, tool string
		wantCount     int
	}{
		// Codex declares Hook.PromptShadowsTranscript: the hook row folds.
		{"s-codex", "codex", 2},
		// Claude Code does not: all three rows stay prompts.
		{"s-cc", "claude-code", 3},
	}
	for _, c := range cases {
		if err := s.UpsertSession(ctx, models.Session{ID: c.session, ProjectID: pid, Tool: c.tool, StartedAt: base}); err != nil {
			t.Fatal(err)
		}
		rows := []struct {
			srcFile, srcEvent, msgID string
			at                       time.Duration
		}{
			{c.tool + ":hook", c.session + ":UserPromptSubmit:1", "turn-1", 0},
			{"/rollouts/" + c.session + ".jsonl", c.session + ":line:1", "turn-1", 2 * time.Second},
			{"/rollouts/" + c.session + ".jsonl", c.session + ":line:9", "turn-2", time.Minute},
		}
		for _, r := range rows {
			if _, err := database.ExecContext(ctx, `
				INSERT INTO actions (session_id, project_id, timestamp, action_type, tool, success, target,
				                     raw_tool_name, error_message, source_file, source_event_id, message_id)
				VALUES (?, ?, ?, 'user_prompt', ?, 1, 'hello', '', '', ?, ?, ?)`,
				c.session, pid, base.Add(r.at).Format(time.RFC3339Nano), c.tool, r.srcFile, r.srcEvent, r.msgID); err != nil {
				t.Fatalf("insert action: %v", err)
			}
		}
	}

	counts, err := s.LoadProjectPromptCounts(ctx, pid, base.Add(-time.Hour), base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if counts[c.session] != c.wantCount {
			t.Errorf("LoadProjectPromptCounts[%s] = %d, want %d", c.session, counts[c.session], c.wantCount)
		}
		times, err := loadUserPromptTimes(ctx, database, c.session)
		if err != nil {
			t.Fatal(err)
		}
		if len(times) != c.wantCount {
			t.Errorf("loadUserPromptTimes(%s) = %d boundaries, want %d", c.session, len(times), c.wantCount)
		}
		for i := 1; i < len(times); i++ {
			if times[i].Before(times[i-1]) {
				t.Errorf("loadUserPromptTimes(%s) not ascending: %v", c.session, times)
			}
		}
	}
}
