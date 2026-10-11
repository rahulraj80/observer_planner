//go:build !sbo_omit_adapter_cursor

package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/pidbridge"
)

func TestSeedCursorSessionPidbridge_WritesBridge(t *testing.T) {
	var got []pidbridge.Entry
	var stderr bytes.Buffer
	body := []byte(`{"hook_event_name":"sessionStart","conversation_id":"cv-1","workspace_roots":["/ws"],"source":"startup"}`)
	seedCursorSessionPidbridge(context.Background(), "sessionStart", body, 4242,
		ancestorsList(4242), captureWriter(&got), nil, &stderr)
	if len(got) != 1 {
		t.Fatalf("entries = %d; want 1", len(got))
	}
	if got[0].SessionID != "cv-1" || got[0].Tool != models.ToolCursor || got[0].CWD != "/ws" {
		t.Errorf("entry = %+v", got[0])
	}
}

func TestSeedCursorSessionPidbridge_BackgroundAgentSkipped(t *testing.T) {
	var got []pidbridge.Entry
	var stderr bytes.Buffer
	body := []byte(`{"hook_event_name":"sessionStart","conversation_id":"cv-2","is_background_agent":true}`)
	seedCursorSessionPidbridge(context.Background(), "sessionStart", body, 4242,
		ancestorsList(4242), captureWriter(&got), nil, &stderr)
	if len(got) != 0 {
		t.Fatalf("entries = %d; want 0 for a background agent", len(got))
	}
}

func TestSeedCursorSessionPidbridge_NonSessionStartNoWrite(t *testing.T) {
	var got []pidbridge.Entry
	var stderr bytes.Buffer
	body := []byte(`{"hook_event_name":"beforeShellCommand","conversation_id":"cv-3","command":"ls"}`)
	seedCursorSessionPidbridge(context.Background(), "beforeShellCommand", body, 4242,
		ancestorsList(4242), captureWriter(&got), nil, &stderr)
	if len(got) != 0 {
		t.Fatalf("entries = %d; want 0 for non-sessionStart", len(got))
	}
}
