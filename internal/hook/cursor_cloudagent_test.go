//go:build !sbo_omit_adapter_cursor

package hook

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"

	"github.com/marmutapp/superbased-observer/internal/adapter/cursor"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// cloudAgentHookPayload is a Cursor 3.23.12-shaped Cloud Agent payload
// (anonymized ids, no real text): root-less, generation_id empty and
// is_background_agent FALSE, exactly as grounded live on 2026-10-05.
func cloudAgentHookPayload(event, extra string) string {
	const conv = "bc-0a0b0c0d-1111-5222-8333-444455556666"
	return `{"conversation_id":"` + conv + `","generation_id":"",` + extra +
		`"model":"cursor-grok-4.6-high","is_background_agent":false,"composer_mode":"chat",` +
		`"session_id":"` + conv + `","hook_event_name":"` + event + `","cursor_version":"3.23.12",` +
		`"workspace_roots":[],"user_email":"dev@example.com","transcript_path":null}`
}

// TestCursorCloudAgentHookLandsSessionAndThought drives the live hook
// receiver against the REAL store: before the fix the sessionStart was
// dropped ("missing project_root or session_id") and the thought was only
// stashed, so a 3.23.12 Cloud Agent left no session row at all.
func TestCursorCloudAgentHookLandsSessionAndThought(t *testing.T) {
	ctx := context.Background()
	database, err := dbtemplate.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "o.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	st := store.New(database)
	const conv = "bc-0a0b0c0d-1111-5222-8333-444455556666"

	var stdout, stderr bytes.Buffer
	HandleCursorEvent(cursor.EventSessionStart, st, nil, strings.NewReader(cloudAgentHookPayload(cursor.EventSessionStart, "")),
		&stdout, &stderr, 5*time.Second)
	if strings.Contains(stderr.String(), "missing project_root") {
		t.Fatalf("cloud sessionStart dropped: %s", stderr.String())
	}
	thought := cloudAgentHookPayload(cursor.EventAfterAgentThought, `"text":"planning the next step","duration_ms":759,`)
	if !cursor.ThoughtMintsRow([]byte(thought)) {
		t.Fatal("ThoughtMintsRow = false for a root-less cloud thought")
	}
	// The database-free reply path (nil sink) must not panic on the row
	// the builder now produces; the caller captures it after the reply.
	stdout.Reset()
	HandleCursorEvent(cursor.EventAfterAgentThought, nil, nil, strings.NewReader(thought), &stdout, &stderr, 0)
	if !strings.Contains(stdout.String(), `"continue":true`) {
		t.Fatalf("thought reply = %q", stdout.String())
	}
	CaptureCursorEvent(cursor.EventAfterAgentThought, []byte(thought), st, nil, &stderr, 5*time.Second)

	root, err := st.ProjectRootForSession(ctx, conv)
	if err != nil {
		t.Fatal(err)
	}
	if root != cursor.SyntheticProjectRoot {
		t.Fatalf("session root = %q, want %q (stderr %q)", root, cursor.SyntheticProjectRoot, stderr.String())
	}
	rows, err := database.QueryContext(ctx, `SELECT action_type, COALESCE(preceding_reasoning, ''), COALESCE(duration_ms, 0)
		FROM actions WHERE session_id = ? ORDER BY id`, conv)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var at, reasoning string
		var dur int64
		if err := rows.Scan(&at, &reasoning, &dur); err != nil {
			t.Fatal(err)
		}
		got = append(got, at)
		if at == models.ActionHarnessCall && (reasoning != "planning the next step" || dur != 759) {
			t.Fatalf("thought row reasoning=%q duration=%d", reasoning, dur)
		}
	}
	if len(got) != 2 || got[0] != models.ActionSessionStart || got[1] != models.ActionHarnessCall {
		t.Fatalf("actions = %v, want [session_start harness_call]", got)
	}
}

// TestCursorDraftSessionStartStillExcluded: the draft / local exclusion is
// unchanged, so a draft's root-less sessionStart never conjures a session.
func TestCursorDraftSessionStartStillExcluded(t *testing.T) {
	sink := &fakeSink{}
	var stdout, stderr bytes.Buffer
	body := `{"conversation_id":"draft-0a0b0c0d-1111-4222-8333-444455556666","hook_event_name":"sessionStart","workspace_roots":[]}`
	HandleCursorEvent(cursor.EventSessionStart, sink, nil, strings.NewReader(body), &stdout, &stderr, 250*time.Millisecond)
	if len(sink.called) != 0 {
		t.Fatalf("draft sessionStart ingested %+v", sink.called)
	}
}
