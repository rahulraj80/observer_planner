//go:build !sbo_omit_adapter_codex && !sbo_omit_adapter_open_interpreter

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/pidbridge"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestHandleCodexUserPromptSubmit_EndToEnd mirrors the Claude Code
// end-to-end test, pinned against Codex's top-level-block dialect
// (legacy {"decision":"block","reason":…}): register (wired into
// handleCodexHook below) → a live-shaped secret → a block reply → an
// identical (same finding set) resend → no decision field (allowed).
func TestHandleCodexUserPromptSubmit_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "observer.db")
	configPath := filepath.Join(dir, "config.toml")
	cfgBody := "[observer]\ndb_path = " + strconv.Quote(filepath.ToSlash(dbPath)) + "\n\n" +
		"[guard]\nenabled = true\nmode = \"enforce\"\n\n" +
		"[guard.prompt]\nenabled = true\nmode = \"ask-once\"\nhook_lane = true\nreconsider_min_delay = \"0s\"\n"
	if err := os.WriteFile(configPath, []byte(cfgBody), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	const secretPrompt = `{"session_id":"s1","cwd":"/r","hook_event_name":"UserPromptSubmit","prompt":"my key is sk-ant-api03-1234567890ABCDEFGhijKLmnopqRSTuvwxyz"}`
	const secretPromptResend = `{"session_id":"s1","cwd":"/r","hook_event_name":"UserPromptSubmit","prompt":"sending again: my key is sk-ant-api03-1234567890ABCDEFGhijKLmnopqRSTuvwxyz"}`

	runOnce := func(payload string) map[string]any {
		t.Helper()
		stdinR, stdinW, err := os.Pipe()
		if err != nil {
			t.Fatalf("pipe: %v", err)
		}
		go func() {
			_, _ = stdinW.Write([]byte(payload))
			_ = stdinW.Close()
		}()
		stdoutR, stdoutW, err := os.Pipe()
		if err != nil {
			t.Fatalf("pipe: %v", err)
		}
		oldStdin, oldStdout := os.Stdin, os.Stdout
		os.Stdin, os.Stdout = stdinR, stdoutW
		handleCodexHook(context.Background(), "UserPromptSubmit", configPath)
		os.Stdin, os.Stdout = oldStdin, oldStdout
		_ = stdoutW.Close()
		out, _ := io.ReadAll(stdoutR)
		var reply map[string]any
		if err := json.Unmarshal(out, &reply); err != nil {
			t.Fatalf("reply not JSON: %v (%q)", err, out)
		}
		return reply
	}

	first := runOnce(secretPrompt)
	if got, _ := first["decision"].(string); got != "block" {
		t.Fatalf("first submission decision = %q, want block (reply=%+v)", got, first)
	}
	if reason, _ := first["reason"].(string); reason == "" {
		t.Fatalf("first submission carried no reason")
	} else if strings.Contains(reason, "sk-ant-api03-1234567890ABCDEFGhijKLmnopqRSTuvwxyz") {
		t.Fatalf("reason leaked the raw secret value: %q", reason)
	}

	second := runOnce(secretPromptResend)
	if _, hasDecision := second["decision"]; hasDecision {
		t.Fatalf("identical resend must carry NO decision field (allowed), got %+v", second)
	}

	ctx := context.Background()
	database, err := dbtemplate.Open(ctx, db.Options{Path: dbPath})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	events, err := store.New(database).LoadRecentGuardEvents(ctx, time.Time{}, 10)
	if err != nil {
		t.Fatalf("LoadRecentGuardEvents: %v", err)
	}
	found := false
	for _, e := range events {
		if e.RuleID == "R-172" && e.Tool == models.ToolCodex {
			found = true
		}
	}
	if !found {
		t.Errorf("no codex R-172 guard_events row found among %+v", events)
	}

	// NIT (phase-2 review): the guard verdict is not the only thing
	// that must land - a denied prompt is still an attempt worth
	// recording (same posture as every other guarded receiver), so
	// BOTH resends must also produce an actions row via the EXISTING
	// codexadapter.BuildHookEvent -> Ingest capture path (handled in
	// the "handled" branch of handleCodexHook, unconditional on the
	// verdict).
	var actionCount int
	if err := database.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM actions WHERE session_id = ? AND action_type = ?",
		"s1", string(models.ActionUserPrompt),
	).Scan(&actionCount); err != nil {
		t.Fatalf("query actions: %v", err)
	}
	if actionCount != 2 {
		t.Errorf("actions row count for session s1/action_type=user_prompt = %d, want 2 (one per submission, guard verdict notwithstanding)", actionCount)
	}
}

func TestSeedCodexSessionPidbridge_WritesBridge(t *testing.T) {
	var got []pidbridge.Entry
	var stderr bytes.Buffer
	body := []byte(`{"session_id":"cx-1","cwd":"/cx","hook_event_name":"SessionStart"}`)
	seedCodexSessionPidbridge(context.Background(), "SessionStart", body, 4242,
		ancestorsList(4242, 100), captureWriter(&got), &stderr)
	if len(got) != 2 {
		t.Fatalf("entries = %d; want 2", len(got))
	}
	if got[0].SessionID != "cx-1" || got[0].Tool != models.ToolCodex || got[0].CWD != "/cx" {
		t.Errorf("entry[0] = %+v", got[0])
	}
	if got[0].PID != 4242 || got[1].PID != 100 {
		t.Errorf("pids = %d,%d; want 4242,100", got[0].PID, got[1].PID)
	}
}

func TestSeedCodexSessionPidbridge_NonSessionStartNoWrite(t *testing.T) {
	var got []pidbridge.Entry
	var stderr bytes.Buffer
	body := []byte(`{"session_id":"cx-2","hook_event_name":"PreToolUse"}`)
	seedCodexSessionPidbridge(context.Background(), "PreToolUse", body, 4242,
		ancestorsList(4242), captureWriter(&got), &stderr)
	if len(got) != 0 {
		t.Fatalf("entries = %d; want 0 for non-SessionStart", len(got))
	}
}
