package telemetry

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/telemetry/contract"
)

// TestDerivePromptCycles seeds the real store's shapes: a prompt recorded
// twice a moment apart (hook + transcript), a second prompt a minute later,
// token rows from two model families with cache reads, and a third prompt
// whose session is still active. Expect exactly two cycles, content-free and
// contract-valid, with sizes, cache share and end-to-end time from the rows.
func TestDerivePromptCycles(t *testing.T) {
	database := openDB(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO projects (id, root_path, created_at) VALUES (1, '/tmp/proj', '2026-09-25T10:00:00Z')`,
		`INSERT INTO sessions (id, project_id, tool, model, started_at) VALUES ('ses-c', 1, 'claude-code', 'claude-sonnet-4-5', '2026-09-25T11:00:00Z')`,
		`INSERT INTO actions (id, session_id, project_id, timestamp, action_type, tool, success, raw_tool_input) VALUES
		 (10, 'ses-c', 1, '2026-09-25T11:00:00Z',     'user_prompt', 'claude-code', 1, 'please refactor the parser'),
		 (11, 'ses-c', 1, '2026-09-25T11:00:00.700Z', 'user_prompt', 'claude-code', 1, 'please refactor the parser'),
		 (12, 'ses-c', 1, '2026-09-25T11:01:00Z',     'user_prompt', 'claude-code', 1, '<task-notification> agent finished </task-notification>'),
		 (13, 'ses-c', 1, '2026-09-25T11:55:00Z',     'user_prompt', 'claude-code', 1, 'continue')`,
		`INSERT INTO token_usage (session_id, timestamp, tool, model, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens, source) VALUES
		 ('ses-c', '2026-09-25T11:00:05Z', 'claude-code', 'claude-sonnet-4-5', 100, 50, 300, 100, 'test'),
		 ('ses-c', '2026-09-25T11:00:20Z', 'claude-code', 'claude-sonnet-4-5', 0, 20, 0, 0, 'test'),
		 ('ses-c', '2026-09-25T11:01:10Z', 'claude-code', 'gpt-5', 40, 10, 0, 0, 'test'),
		 ('ses-c', '2026-09-25T11:55:30Z', 'claude-code', 'gpt-5', 40, 10, 0, 0, 'test')`,
	} {
		if _, err := database.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	c := testCollector(t, database, contract.TierBaseline) // now = 12:00, last activity 11:55:30
	if _, err := c.Derive(ctx); err != nil {
		t.Fatalf("Derive: %v", err)
	}
	// A second pass must not re-stage the same cycles.
	if _, err := c.Derive(ctx); err != nil {
		t.Fatalf("Derive 2: %v", err)
	}
	rows, err := database.Query(`SELECT payload, dedup_key FROM telemetry_staged WHERE event = 'prompt_cycle' ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []map[string]any
	var keys []string
	for rows.Next() {
		var payload, key string
		if err := rows.Scan(&payload, &key); err != nil {
			t.Fatal(err)
		}
		var f map[string]any
		if err := json.Unmarshal([]byte(payload), &f); err != nil {
			t.Fatal(err)
		}
		got, keys = append(got, f), append(keys, key)
	}
	if len(got) != 3 {
		t.Fatalf("staged %d prompt_cycle events (keys %v); want 3 (cycle 1 trigger + tool_call, cycle 2 trigger; the third cycle is still active)", len(got), keys)
	}
	want := []map[string]any{
		// Cycle 1 trigger: prompt 11:00:00 -> first response 11:00:05.
		{
			"model": "anthropic", "provider": "anthropic", "input_size": 500.0, "output_size": 50.0,
			"cache_percentage": 60.0, "end_to_end_duration_ms": 5000.0, "segment": "user_prompt", "turn_id": "c10",
		},
		// Cycle 1 tool_call remainder: 11:00:05 -> 11:00:20.
		{
			"model": "anthropic", "provider": "anthropic", "input_size": 0.0, "output_size": 20.0,
			"cache_percentage": 0.0, "end_to_end_duration_ms": 15000.0, "segment": "tool_call", "turn_id": "c10",
		},
		// Cycle 2: a harness notification, classified locally as a nudge.
		{
			"model": "openai", "provider": "openai", "input_size": 40.0, "output_size": 10.0,
			"cache_percentage": 0.0, "end_to_end_duration_ms": 10000.0, "segment": "system_nudge", "turn_id": "c12",
		},
	}
	for i := range want {
		for k, v := range want[i] {
			if got[i][k] != v {
				t.Errorf("event %d %s = %v; want %v", i, k, got[i][k], v)
			}
		}
		if len(got[i]) != len(want[i]) {
			t.Errorf("event %d carries other fields: %v", i, got[i])
		}
	}
	if keys[0] != "cycle/10/0" || keys[1] != "cycle/10/1" || keys[2] != "cycle/12/0" {
		t.Errorf("dedup keys = %v; want cycle/10/0, cycle/10/1, cycle/12/0 (the duplicate prompt 11 collapsed)", keys)
	}

	// Each payload is contract-valid as a prompt_cycle event.
	iid, _ := c.InstallID(ctx)
	for i, f := range got {
		env := &contract.Envelope{
			EnvelopeVersion: contract.EnvelopeVersion, BatchUUID: "0b5c3d1e-1111-4222-8333-444455556666",
			InstallID: iid, SeqStart: 1, SeqEnd: 1, ClientVersion: "t", OS: "linux", Arch: "amd64",
			EventCount: 1, ClientTS: "2026-09-25T12:00:00Z",
			Events: []contract.Event{{
				Event: contract.EventPromptCycle, EventVersion: 1, SchemaVersion: SchemaVersion,
				TS: "2026-09-25T12:00:00Z", InstallID: iid, Seq: 1,
				Classification: classificationFor(contract.EventPromptCycle), Purpose: purposeFor(contract.EventPromptCycle),
				Fields: f,
			}},
		}
		if err := contract.ValidateEnvelope(env); err != nil {
			t.Errorf("cycle %d not contract-valid: %v", i, err)
		}
	}

	// Once the session goes quiet, the third cycle completes.
	c.SetNow(func() time.Time { return time.Date(2026, 9, 25, 12, 10, 0, 0, time.UTC) })
	if _, err := c.Derive(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = database.QueryRow(`SELECT count(*) FROM telemetry_staged WHERE event = 'prompt_cycle'`).Scan(&n)
	if n != 4 {
		t.Fatalf("after the session went quiet: %d prompt_cycle events; want 4", n)
	}
}

func TestModelFamily(t *testing.T) {
	for in, want := range map[string]string{
		"claude-opus-4-1": "anthropic", "anthropic/claude-3": "anthropic", "gpt-5": "openai",
		"o3-mini": "openai", "gpt-5-codex": "codex", "gemini-2.5-pro": "google",
		"zai-org/GLM-5.3-Flash": "other", "": "other",
	} {
		if got := modelFamily(in); got != want {
			t.Errorf("modelFamily(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestNudgeClassifier(t *testing.T) {
	r := DefaultNudgeRules()
	for text, want := range map[string]string{
		"please refactor the parser": SegmentUserPrompt,
		"continue":                   SegmentUserPrompt,
		"  <task-notification>done</task-notification>":                             SegmentSystemNudge,
		"<system-reminder>x</system-reminder>":                                      SegmentSystemNudge,
		"<SYSTEM-REMINDER>x":                                                        SegmentSystemNudge,
		"Stop hook feedback:\nlint failed":                                          SegmentSystemNudge,
		"<b>bold</b> is a user's own markup":                                        SegmentUserPrompt,
		"This session is being continued from a previous conversation that ran out": SegmentSystemNudge,
	} {
		if got := r.Classify(text); got != want {
			t.Errorf("Classify(%q) = %s; want %s", text, got, want)
		}
	}
	// A local hash signature (from nudge-scan) catches a verbatim template.
	tmpl := "Run the nightly checklist and report back."
	if r.Classify(tmpl) != SegmentUserPrompt {
		t.Fatal("template classified before its signature was added")
	}
	r = r.Merge(NudgeRules{TextHashes: []string{TextHash(tmpl)}})
	if r.Classify("  "+tmpl+"\n") != SegmentSystemNudge {
		t.Fatal("hash signature did not match the trimmed template")
	}
}

func TestNudgeRulesFileRoundTrip(t *testing.T) {
	path := t.TempDir() + "/rules.json"
	if r, err := LoadNudgeRules(path); err != nil || len(r.MarkupTags) == 0 {
		t.Fatalf("missing file: rules=%v err=%v; want defaults", r, err)
	}
	if err := SaveNudgeRules(path, NudgeRules{MarkupTags: []string{"cron-trigger"}}); err != nil {
		t.Fatal(err)
	}
	r, err := LoadNudgeRules(path)
	if err != nil {
		t.Fatal(err)
	}
	if r.Classify("<cron-trigger>nightly</cron-trigger>") != SegmentSystemNudge {
		t.Fatal("local markup tag not applied")
	}
	if r.Classify("<task-notification>x") != SegmentSystemNudge {
		t.Fatal("defaults lost after merging the local file")
	}
}
