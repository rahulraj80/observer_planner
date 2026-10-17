//go:build !sbo_omit_adapter_cursor

package hook

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/adapter/cursor"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestCursorHookStampsToolVersion pins T-784 on the LIVE hook path: the
// payload's cursor_version reaches sessions.tool_version through the one
// store seam (IngestOptions.SessionToolVersions -> SetSessionToolVersion),
// first-wins, and a payload without cursor_version (the 3.4.x shape)
// stamps nothing.
func TestCursorHookStampsToolVersion(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("HOME", profile)
	t.Setenv("USERPROFILE", profile)
	ctx := context.Background()
	database, err := dbtemplate.Open(ctx, db.Options{Path: filepath.Join(t.TempDir(), "o.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	st := store.New(database)
	run := func(event, body string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		HandleCursorEvent(event, st, nil, strings.NewReader(body), &stdout, &stderr, 5*time.Second)
		if stderr.Len() > 0 {
			t.Fatalf("%s stderr: %s", event, stderr.String())
		}
	}

	run(cursor.EventBeforeSubmitPrompt, `{"conversation_id":"tv-c1","generation_id":"g1","prompt":"hi",
		"workspace_roots":["/tmp/tv-proj"],"hook_event_name":"beforeSubmitPrompt","cursor_version":"3.20.21"}`)
	got, err := st.LoadSessionToolVersion(ctx, "tv-c1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "3.20.21" {
		t.Fatalf("tool_version = %q, want 3.20.21", got)
	}

	// A later event from a different build never overwrites (first-wins).
	run(cursor.EventBeforeSubmitPrompt, `{"conversation_id":"tv-c1","generation_id":"g2","prompt":"again",
		"workspace_roots":["/tmp/tv-proj"],"hook_event_name":"beforeSubmitPrompt","cursor_version":"3.21.13"}`)
	if got, _ := st.LoadSessionToolVersion(ctx, "tv-c1"); got != "3.20.21" {
		t.Fatalf("tool_version after second build = %q, want 3.20.21 kept", got)
	}

	// No cursor_version (3.4.x payloads): the honest unknown stays empty.
	run(cursor.EventBeforeSubmitPrompt, `{"conversation_id":"tv-c2","generation_id":"g1","prompt":"hi",
		"workspace_roots":["/tmp/tv-proj"],"hook_event_name":"beforeSubmitPrompt"}`)
	if got, err := st.LoadSessionToolVersion(ctx, "tv-c2"); err != nil || got != "" {
		t.Fatalf("tool_version = %q, %v; want empty", got, err)
	}
}
