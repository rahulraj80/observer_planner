//go:build !sbo_omit_adapter_hermes

package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/pidbridge"
)

func TestSeedHermesSessionPidbridge_UsesPluginPID(t *testing.T) {
	var got []pidbridge.Entry
	var stderr bytes.Buffer
	// Plugin supplies pid=7777; the ancestor walk (100,200) must be
	// bypassed in favour of the single plugin pid.
	body := []byte(`{"event":"session_start","session_id":"hz-1","cwd":"/hz","pid":7777,"ppid":100}`)
	seedHermesSessionPidbridge(context.Background(), "session_start", body, 999,
		ancestorsList(100, 200), captureWriter(&got), &stderr)
	if len(got) != 1 {
		t.Fatalf("entries = %d; want 1 (plugin pid only)", len(got))
	}
	if got[0].PID != 7777 || got[0].SessionID != "hz-1" || got[0].Tool != models.ToolHermes {
		t.Errorf("entry = %+v; want pid=7777 hz-1 hermes", got[0])
	}
}

func TestSeedHermesSessionPidbridge_FallsBackToAncestorWalk(t *testing.T) {
	var got []pidbridge.Entry
	var stderr bytes.Buffer
	// Older plugin: no pid field → ancestor-walk the exec'd hook child.
	body := []byte(`{"event":"session_start","session_id":"hz-2","cwd":"/hz"}`)
	seedHermesSessionPidbridge(context.Background(), "session_start", body, 999,
		ancestorsList(555, 556), captureWriter(&got), &stderr)
	if len(got) != 2 {
		t.Fatalf("entries = %d; want 2 (ancestor walk)", len(got))
	}
	if got[0].PID != 555 || got[1].PID != 556 || got[0].Tool != models.ToolHermes {
		t.Errorf("entries = %+v; want 555,556 hermes", got)
	}
}

func TestSeedHermesSessionPidbridge_NonSessionStartNoWrite(t *testing.T) {
	var got []pidbridge.Entry
	var stderr bytes.Buffer
	body := []byte(`{"event":"tool_call","session_id":"hz-3","pid":7777}`)
	seedHermesSessionPidbridge(context.Background(), "tool_call", body, 999,
		ancestorsList(1, 2), captureWriter(&got), &stderr)
	if len(got) != 0 {
		t.Fatalf("entries = %d; want 0 for non-session_start", len(got))
	}
}
