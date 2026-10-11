package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// seedT635PromptPairs re-tags a seeded session's tool and plants one prompt
// per entry of times, recorded twice the way Codex records it: the
// UserPromptSubmit hook row ("<tool>:hook") and, two seconds later, its
// transcript twin naming the same turn.
func seedT635PromptPairs(t *testing.T, st *Store, sessionID, tool string, times []time.Time, sidechain bool) {
	t.Helper()
	ctx := context.Background()
	if _, err := st.db.ExecContext(ctx, `UPDATE sessions SET tool = ? WHERE id = ?`, tool, sessionID); err != nil {
		t.Fatalf("retag session: %v", err)
	}
	var projectID int64
	if err := st.db.QueryRowContext(ctx, `SELECT project_id FROM sessions WHERE id = ?`, sessionID).Scan(&projectID); err != nil {
		t.Fatalf("project id: %v", err)
	}
	side := 0
	if sidechain {
		side = 1
	}
	for i, at := range times {
		turn := fmt.Sprintf("turn-%d", i)
		body := fmt.Sprintf("t635 prompt %d of %s with enough words to be distinct", i, sessionID)
		rows := []struct {
			srcFile, srcEvent string
			at                time.Time
		}{
			{tool + ":hook", fmt.Sprintf("%s:UserPromptSubmit:%d", sessionID, i), at},
			{"/rollouts/" + sessionID + ".jsonl", fmt.Sprintf("%s:line:%d", sessionID, i), at.Add(2 * time.Second)},
		}
		for _, r := range rows {
			if _, err := st.db.ExecContext(ctx, `
				INSERT INTO actions (session_id, project_id, timestamp, action_type, tool, success, target,
				                     raw_tool_name, error_message, source_file, source_event_id, message_id,
				                     is_sidechain)
				VALUES (?, ?, ?, 'user_prompt', ?, 1, ?, '', '', ?, ?, ?, ?)`,
				sessionID, projectID, r.at.UTC().Format(time.RFC3339Nano), tool, body,
				r.srcFile, r.srcEvent, turn, side); err != nil {
				t.Fatalf("insert prompt: %v", err)
			}
		}
	}
}

// TestT635PriorFoldsHookPromptShadows pins T-635 (the predictor half): the
// cross-session turns-per-message prior divided a session's turns by its RAW
// user_prompt row count, so a Codex prompt recorded by both its hook and its
// transcript counted as two messages and halved the fan-out. The divisor is
// now the canonical prompt count from the one pairing table
// (sessionmsg.CanonicalPrompts); a tool that does not declare the shadow
// keeps every row.
func TestT635PriorFoldsHookPromptShadows(t *testing.T) {
	cases := []struct {
		tool string
		want int
	}{
		// 4 turns over 2 prompts, each recorded twice: the hook rows fold.
		{"codex", 2},
		// Claude Code declares no shadow: 4 rows are 4 prompts, 4 / 4 = 1.
		{"claude-code", 1},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			st := newPredictTestStore(t)
			ctx := context.Background()
			base := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
			for _, id := range []string{"t635-a", "t635-b", "t635-c"} {
				seedShapeSession(t, st, id, "", []seedRow{
					{source: "proxy", ts: "2026-08-20T10:00:05Z", model: "gpt-5.4", input: 100, output: 10},
					{source: "proxy", ts: "2026-08-20T10:00:06Z", model: "gpt-5.4", input: 100, output: 10},
					{source: "proxy", ts: "2026-08-20T10:01:05Z", model: "gpt-5.4", input: 100, output: 10},
					{source: "proxy", ts: "2026-08-20T10:01:06Z", model: "gpt-5.4", input: 100, output: 10},
				}, nil)
				seedT635PromptPairs(t, st, id, c.tool, []time.Time{base, base.Add(time.Minute)}, false)
			}
			prior, err := st.LoadToolProjectPrior(ctx, c.tool, 1, 0)
			if err != nil {
				t.Fatalf("LoadToolProjectPrior: %v", err)
			}
			if len(prior) != 3 {
				t.Fatalf("prior = %v, want 3 samples", prior)
			}
			for i, v := range prior {
				if v != c.want {
					t.Errorf("prior[%d] = %d, want %d (%s)", i, v, c.want, c.tool)
				}
			}
		})
	}
}

// TestT635CloudPromptSamplerFoldsHookPromptShadows pins T-635 (the cloud
// half): the evidence prompt sampler counted and sampled raw user_prompt
// rows, so every Codex prompt reached the excerpt pool twice (once from the
// hook, once from the transcript) and the hook copies spent the sample
// budget. It now samples the canonical prompts; sidechain rows stay out.
func TestT635CloudPromptSamplerFoldsHookPromptShadows(t *testing.T) {
	s, _ := cloudTestStore(t)
	ctx := context.Background()
	seedEligibleSession(t, s, "t635-cloud")
	base := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	const n = 5
	var times []time.Time
	for i := 0; i < n; i++ {
		times = append(times, base.Add(time.Duration(i)*time.Minute))
	}
	seedT635PromptPairs(t, s, "t635-cloud", "codex", times, false)
	// A sub-agent brief on the same session never reaches the content pool.
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO actions (session_id, project_id, timestamp, action_type, tool, success, target,
		                     raw_tool_name, error_message, source_file, source_event_id, is_sidechain)
		SELECT id, project_id, ?, 'user_prompt', 'codex', 1, 'sidechain brief', '', '', '/rollouts/x.jsonl', 'side-1', 1
		  FROM sessions WHERE id = 't635-cloud'`, base.Add(30*time.Second).Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert sidechain: %v", err)
	}

	texts, err := s.LoadCloudSessionTexts(ctx, "t635-cloud")
	if err != nil {
		t.Fatalf("LoadCloudSessionTexts: %v", err)
	}
	if len(texts.UserPrompts) != n {
		t.Fatalf("UserPrompts = %d (%q), want %d (one per prompt typed)", len(texts.UserPrompts), texts.UserPrompts, n)
	}
	for i, p := range texts.UserPrompts {
		want := fmt.Sprintf("t635 prompt %d of t635-cloud with enough words to be distinct", i)
		if p != want {
			t.Errorf("UserPrompts[%d] = %q, want %q", i, p, want)
		}
	}
}

// TestT635CloudPromptSamplerBoundOnFoldedPopulation pins the sampler's head
// plus stride arithmetic over the folded population: past the bound the
// first and the LAST canonical prompt are still returned, no hook copy is.
func TestT635CloudPromptSamplerBoundOnFoldedPopulation(t *testing.T) {
	s, _ := cloudTestStore(t)
	ctx := context.Background()
	seedEligibleSession(t, s, "t635-big")
	base := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	const n = 300
	var times []time.Time
	for i := 0; i < n; i++ {
		times = append(times, base.Add(time.Duration(i)*time.Minute))
	}
	seedT635PromptPairs(t, s, "t635-big", "codex", times, false)
	texts, err := s.LoadCloudSessionTexts(ctx, "t635-big")
	if err != nil {
		t.Fatalf("LoadCloudSessionTexts: %v", err)
	}
	if len(texts.UserPrompts) == 0 || len(texts.UserPrompts) > cloudMaxPromptRows {
		t.Fatalf("returned %d prompts, want 1..%d", len(texts.UserPrompts), cloudMaxPromptRows)
	}
	seen := map[string]bool{}
	for _, p := range texts.UserPrompts {
		if seen[p] {
			t.Fatalf("prompt %q returned twice: a hook copy reached the sample", p)
		}
		seen[p] = true
	}
	first := "t635 prompt 0 of t635-big with enough words to be distinct"
	last := fmt.Sprintf("t635 prompt %d of t635-big with enough words to be distinct", n-1)
	if texts.UserPrompts[0] != first {
		t.Errorf("first = %q, want %q", texts.UserPrompts[0], first)
	}
	if texts.UserPrompts[len(texts.UserPrompts)-1] != last {
		t.Errorf("last = %q, want %q", texts.UserPrompts[len(texts.UserPrompts)-1], last)
	}
}

// BenchmarkT635Prior100 measures LoadToolProjectPrior over a full 100-session
// prior (the LIMIT): 100 Codex sessions, each 20 prompts recorded by hook and
// transcript plus 40 proxied turns.
func BenchmarkT635Prior100(b *testing.B) {
	st := newPredictTestStore(b)
	ctx := context.Background()
	base := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	for s := 0; s < 100; s++ {
		id := fmt.Sprintf("b635-%03d", s)
		if _, err := st.db.ExecContext(ctx, `INSERT OR IGNORE INTO projects (id, root_path, created_at)
		      VALUES (1, '/tmp/shape', '2026-08-01T00:00:00Z')`); err != nil {
			b.Fatal(err)
		}
		if _, err := st.db.ExecContext(ctx, `INSERT INTO sessions (id, project_id, tool, started_at)
		      VALUES (?, 1, 'codex', ?)`, id, base.Add(time.Duration(s)*time.Hour).Format(time.RFC3339)); err != nil {
			b.Fatal(err)
		}
		for p := 0; p < 20; p++ {
			at := base.Add(time.Duration(s)*time.Hour + time.Duration(p)*time.Minute)
			for k, src := range []string{"codex:hook", "/rollouts/" + id + ".jsonl"} {
				if _, err := st.db.ExecContext(ctx, `
					INSERT INTO actions (session_id, project_id, timestamp, action_type, tool, success,
					                     source_file, source_event_id, message_id)
					VALUES (?, 1, ?, 'user_prompt', 'codex', 1, ?, ?, ?)`,
					id, at.Add(time.Duration(k)*2*time.Second).Format(time.RFC3339Nano), src,
					fmt.Sprintf("%s:%d:%d", id, p, k), fmt.Sprintf("turn-%d", p)); err != nil {
					b.Fatal(err)
				}
			}
			for k := 0; k < 2; k++ {
				if _, err := st.db.ExecContext(ctx, `
					INSERT INTO api_turns (session_id, timestamp, provider, model, input_tokens, output_tokens)
					VALUES (?, ?, 'openai', 'gpt-5.4', 100, 10)`,
					id, at.Add(time.Duration(5+k)*time.Second).Format(time.RFC3339Nano)); err != nil {
					b.Fatal(err)
				}
			}
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prior, err := st.LoadToolProjectPrior(ctx, "codex", 1, 0)
		if err != nil {
			b.Fatal(err)
		}
		if len(prior) != 100 {
			b.Fatalf("prior = %d samples, want 100", len(prior))
		}
	}
}
