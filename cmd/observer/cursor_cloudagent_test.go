//go:build !sbo_omit_adapter_cursor

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/adapter/cursor"
	"github.com/marmutapp/superbased-observer/internal/pidbridge"
)

// TestSeedCursorSessionPidbridge_CloudAgentIDSkipped pins the slow no-op
// sessionStart (19.85 s on 2026-10-05): Cursor 3.23.12 sends
// is_background_agent=false for a `bc-` Cloud Agent, so the flag-only skip
// let the seed register the hook bridge's own relay pids, each through its
// own config load, database open and unbounded write.
func TestSeedCursorSessionPidbridge_CloudAgentIDSkipped(t *testing.T) {
	var got []pidbridge.Entry
	var stderr bytes.Buffer
	body := []byte(`{"hook_event_name":"sessionStart","conversation_id":"bc-0a0b0c0d-1111-5222-8333-444455556666",` +
		`"is_background_agent":false,"composer_mode":"chat","workspace_roots":[]}`)
	seedCursorSessionPidbridge(context.Background(), "sessionStart", body, 4242,
		ancestorsList(4242, 4241), captureWriter(&got), nil, &stderr)
	if len(got) != 0 {
		t.Fatalf("entries = %d (%+v); want 0 for a Cloud Agent conversation", len(got), got)
	}
}

// TestCursorCloudThoughtReplyThenCapture: a Cloud Agent thought still
// replies with no database open (an unusable config cannot block the
// reply), and only then tries its row capture.
func TestCursorCloudThoughtReplyThenCapture(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	configPath := filepath.Join(dir, "invalid.toml")
	if err := os.WriteFile(configPath, []byte("[invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	input, err := os.CreateTemp(dir, "input")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.CreateTemp(dir, "output")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	body := `{"conversation_id":"bc-0a0b0c0d-1111-5222-8333-444455556666","generation_id":"","text":"step","duration_ms":5,"workspace_roots":[]}`
	if !cursor.ThoughtMintsRow([]byte(body)) {
		t.Fatal("fixture is not a row-minting cloud thought")
	}
	if _, err := input.WriteString(body); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	oldInput, oldOutput := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = input, output
	defer func() { os.Stdin, os.Stdout = oldInput, oldOutput }()
	handleCursorHook(context.Background(), cursor.EventAfterAgentThought, configPath)
	reply, err := os.ReadFile(output.Name())
	if err != nil || !strings.Contains(string(reply), `"continue":true`) {
		t.Fatalf("cloud thought reply %q: %v", reply, err)
	}
}
