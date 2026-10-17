package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/db/dbtemplate"

	"github.com/marmutapp/superbased-observer/internal/adapter/cursor"
	"github.com/marmutapp/superbased-observer/internal/db"
	"github.com/marmutapp/superbased-observer/internal/diag"
	"github.com/marmutapp/superbased-observer/internal/integration"
	"github.com/marmutapp/superbased-observer/internal/store"
)

// TestClassifyToolGateReply: the generic reply table recognises each
// vendor's deny / ask spelling and never reads an allow as a block.
func TestClassifyToolGateReply(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stdout  string
		code    int
		blocked bool
	}{
		{"claude deny", claudePreToolDeny("x"), 0, true},
		{"claude ask", `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask"}}`, 0, true},
		{"claude allow", `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}`, 0, false},
		{"approve", `{"decision":"approve"}`, 0, false},
		{"cursor deny", `{"permission":"deny","continue":true}`, 0, true},
		{"cursor ask", `{"permission":"ask","continue":true}`, 0, true},
		{"cursor allow", `{"permission":"allow","continue":true}`, 0, false},
		{"snake case", `{"hook_specific_output":{"permission_decision":"deny"}}`, 0, true},
		{"top-level block", `{"decision":"block","reason":"r"}`, 0, true},
		{"continue false", `{"continue":false}`, 0, true},
		{"exit-code dialect", ``, 2, true},
		{"empty allow", ``, 0, false},
		{"garbage", `not json`, 0, false},
	} {
		got, detail := classifyToolGateReply([]byte(tc.stdout), tc.code)
		if got != tc.blocked {
			t.Errorf("%s: blocked = %v (%s), want %v", tc.name, got, detail, tc.blocked)
		}
	}
}

// TestToolGatePayloadsParse: each known dialect's payload is read by its
// own receiver's extractor as a call on the fake path; MCP is not probeable;
// an unknown dialect gets the superset payload.
func TestToolGatePayloadsParse(t *testing.T) {
	fake := toolGateProbePath("/home/u")
	in := func(ev string) toolGateProbeInput {
		return toolGateProbeInput{Event: ev, SessionID: "observer-doctor-probe-x", FakePath: fake, Cwd: "/work"}
	}
	var claude struct {
		SessionID string `json:"session_id"`
		ToolName  string `json:"tool_name"`
		ToolInput struct {
			FilePath string `json:"file_path"`
		} `json:"tool_input"`
	}
	if err := json.Unmarshal(toolGateProbePayload("claude-code-pretool", in("PreToolUse")), &claude); err != nil ||
		claude.ToolName != "Write" || claude.ToolInput.FilePath != fake || claude.SessionID == "" {
		t.Errorf("claude payload = %+v (%v)", claude, err)
	}
	for _, ev := range []string{"beforeShellExecution", "beforeReadFile"} {
		body := toolGateProbePayload("cursor-permission", in(ev))
		got, ok, err := cursor.BuildEvent(ev, body, nil)
		if err != nil || !ok || !strings.Contains(got.Target, ".ssh") {
			t.Errorf("cursor %s: event %+v ok=%v err=%v", ev, got, ok, err)
		}
	}
	if b := toolGateProbePayload("cursor-permission", in("beforeMCPExecution")); b != nil {
		t.Errorf("an MCP event got a synthetic payload: %s", b)
	}
	if b := toolGateProbePayload("a-dialect-another-lane-adds", in("PreToolUse")); !bytes.Contains(b, []byte(toolGateProbeFakeName)) {
		t.Errorf("generic payload lacks the fake path: %s", b)
	}
}

// TestToolGateProbeCandidatesFollowBindings: the candidates are exactly the
// rows with a wired tool_gate binding.
func TestToolGateProbeCandidatesFollowBindings(t *testing.T) {
	want := map[string]bool{}
	for _, c := range integration.Capabilities() {
		if c.Hook.WiredFor(integration.HookCapToolGate) {
			want[c.Tool] = true
		}
	}
	got := toolGateProbeCandidates()
	if len(got) != len(want) {
		t.Fatalf("candidates %v, want %v", got, want)
	}
	for _, tool := range got {
		if !want[tool] {
			t.Errorf("%s is a candidate without a wired tool gate", tool)
		}
	}
}

// toolGateTestConfig writes a config with its own DB and the given guard
// mode.
func toolGateTestConfig(t *testing.T, mode string) (cfgPath, dbPath string) {
	t.Helper()
	dir := t.TempDir()
	dbPath = filepath.Join(dir, "observer.db")
	cfgPath = filepath.Join(dir, "config.toml")
	body := "[observer]\ndb_path = " + quoteTOMLPath(dbPath) + "\n\n[guard]\nenabled = true\nmode = \"" + mode + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath, dbPath
}

func toolGateTestLookup(t *testing.T, dbPath string) guardEventLookup {
	t.Helper()
	return func(ctx context.Context, sid string) ([]string, error) {
		database, err := dbtemplate.Open(ctx, db.Options{Path: dbPath})
		if err != nil {
			return nil, err
		}
		defer database.Close()
		return storeGuardEventLookup(store.New(database))(ctx, sid)
	}
}

// TestRunToolGateProbes_EndToEnd drives the probe through a real compiled
// binary: blocked under enforcement, evaluated-and-allowed in observe mode,
// NOT REGISTERED on a fresh home, and the MCP channel reported as not
// probeable.
func TestRunToolGateProbes_EndToEnd(t *testing.T) {
	binary := probeHookTestBinary(t)
	ctx := context.Background()
	byEvent := func(cs []diag.Check) map[string]diag.Check {
		m := map[string]diag.Check{}
		for _, c := range cs {
			m[c.Name[strings.LastIndex(c.Name, ":")+1:]] = c
		}
		return m
	}

	t.Run("claude-code blocked under enforce", func(t *testing.T) {
		home := probeHookSetupHome(t)
		cfgPath, dbPath := toolGateTestConfig(t, "enforce")
		probeHookRegister(t, home, binary, cfgPath, "claude-code")
		cs := runToolGateProbes(ctx, binary, cfgPath, "claude-code", toolGateTestLookup(t, dbPath))
		if len(cs) != 1 || cs[0].Status != diag.StatusOK || !strings.Contains(cs[0].Message, "BLOCKED") {
			t.Fatalf("checks = %+v", cs)
		}
		if _, err := os.Stat(toolGateProbePath(home)); !os.IsNotExist(err) {
			t.Errorf("the probe target exists: %v (nothing may be executed)", err)
		}
	})
	t.Run("claude-code evaluated in observe mode", func(t *testing.T) {
		home := probeHookSetupHome(t)
		cfgPath, dbPath := toolGateTestConfig(t, "observe")
		probeHookRegister(t, home, binary, cfgPath, "claude-code")
		cs := runToolGateProbes(ctx, binary, cfgPath, "claude-code", toolGateTestLookup(t, dbPath))
		if len(cs) != 1 || cs[0].Status != diag.StatusWarn || !strings.Contains(cs[0].Message, "EVALUATED") || !strings.Contains(cs[0].Message, "R-152") {
			t.Fatalf("checks = %+v", cs)
		}
	})
	t.Run("claude-code not registered", func(t *testing.T) {
		probeHookSetupHome(t)
		cfgPath, dbPath := toolGateTestConfig(t, "enforce")
		cs := runToolGateProbes(ctx, binary, cfgPath, "claude-code", toolGateTestLookup(t, dbPath))
		if len(cs) != 1 || cs[0].Status != diag.StatusFail || !strings.Contains(cs[0].Message, "NOT REGISTERED") {
			t.Fatalf("checks = %+v", cs)
		}
	})
	t.Run("cursor channels under enforce", func(t *testing.T) {
		home := probeHookSetupHome(t)
		if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o700); err != nil {
			t.Fatal(err)
		}
		cfgPath, dbPath := toolGateTestConfig(t, "enforce")
		probeHookRegister(t, home, binary, cfgPath, "cursor")
		m := byEvent(runToolGateProbes(ctx, binary, cfgPath, "cursor", toolGateTestLookup(t, dbPath)))
		for _, ev := range []string{"beforeShellExecution", "beforeReadFile"} {
			if c := m[ev]; c.Status != diag.StatusOK || !strings.Contains(c.Message, "BLOCKED") {
				t.Errorf("%s: %+v", ev, c)
			}
		}
		if c := m["beforeMCPExecution"]; c.Status != diag.StatusWarn || !strings.Contains(c.Message, "no synthetic probe") {
			t.Errorf("beforeMCPExecution: %+v", c)
		}
	})
	t.Run("guard off: nothing to probe", func(t *testing.T) {
		probeHookSetupHome(t)
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(cfgPath, []byte("[guard]\nenabled = false\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cs := runToolGateProbes(ctx, binary, cfgPath, "claude-code", nil)
		if len(cs) != 1 || cs[0].Status != diag.StatusWarn || !strings.Contains(cs[0].Message, "guard is off") {
			t.Fatalf("checks = %+v", cs)
		}
	})
}
