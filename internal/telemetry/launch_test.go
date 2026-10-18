package telemetry

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/telemetry/contract"
)

// TestDeriveTagsInAppLaunches (Step 5.9): a session correlated to a dashboard
// terminal run at or above the link threshold is "in_app"; a heuristic-only
// (below threshold) or uncorrelated session is "external". The tag rides on
// session_start/session_end/token_use and nothing about the run leaves.
func TestDeriveTagsInAppLaunches(t *testing.T) {
	database := openDB(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO projects (id, root_path, created_at) VALUES (1, '/tmp/proj', '2026-09-25T10:00:00Z')`,
		`INSERT INTO sessions (id, project_id, tool, model, started_at, ended_at, error_rate) VALUES
		 ('ses-app', 1, 'claude-code', 'claude-sonnet-4-5', '2026-09-25T11:00:00Z', '2026-09-25T11:10:00Z', 0),
		 ('ses-weak', 1, 'claude-code', 'claude-sonnet-4-5', '2026-09-25T11:00:00Z', '2026-09-25T11:10:00Z', 0),
		 ('ses-ext', 1, 'claude-code', 'claude-sonnet-4-5', '2026-09-25T11:00:00Z', '2026-09-25T11:10:00Z', 0)`,
		`INSERT INTO terminal_run (run_id, tool, kind, launched_at) VALUES
		 ('run-1', 'claude-code', 'fresh', '2026-09-25T10:59:00Z'),
		 ('run-2', 'claude-code', 'fresh', '2026-09-25T10:59:00Z')`,
		`INSERT INTO terminal_run_session (run_id, session_id, confidence, source, observed_at) VALUES
		 ('run-1', 'ses-app', 0.95, 'oob', '2026-09-25T11:00:05Z'),
		 ('run-2', 'ses-weak', 0.40, 'heuristic', '2026-09-25T11:00:05Z')`,
		`INSERT INTO token_usage (session_id, timestamp, tool, model, input_tokens, output_tokens, cache_read_tokens, source) VALUES
		 ('ses-app', '2026-09-25T11:05:00Z', 'claude-code', 'claude-sonnet-4-5', 100, 40, 0, 'test'),
		 ('ses-ext', '2026-09-25T11:05:00Z', 'claude-code', 'claude-sonnet-4-5', 100, 40, 0, 'test')`,
	} {
		if _, err := database.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	c := testCollector(t, database, contract.TierBaseline)
	if _, err := c.Derive(ctx); err != nil {
		t.Fatalf("Derive: %v", err)
	}
	rows, err := database.Query(`SELECT event, payload, dedup_key FROM telemetry_staged WHERE event IN ('session_start','session_end','token_use')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{} // event/session -> launch
	for rows.Next() {
		var event, payload, key string
		if err := rows.Scan(&event, &payload, &key); err != nil {
			t.Fatal(err)
		}
		var f map[string]any
		_ = json.Unmarshal([]byte(payload), &f)
		session, _, _ := strings.Cut(key, "/") // dedup keys start with the session id
		got[event+"/"+session], _ = f["launch"].(string)
	}
	want := map[string]string{
		"session_start/ses-app": "in_app", "session_end/ses-app": "in_app", "token_use/ses-app": "in_app",
		"session_start/ses-weak": "external", "session_start/ses-ext": "external", "token_use/ses-ext": "external",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s launch = %q; want %q", k, got[k], v)
		}
	}
}
