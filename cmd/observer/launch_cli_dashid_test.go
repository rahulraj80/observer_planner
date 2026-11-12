//go:build !sbo_omit_terminals

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/dashapi"
)

// TestTerminalsCLIRunIDStartingWithDash (live check 2026-11-12): `observer
// terminals stop -XJMm...` failed with "unknown shorthand flag: 'X'". An id
// given after -- reaches the verb, and the flag error says to do that.
func TestTerminalsCLIRunIDStartingWithDash(t *testing.T) {
	requireTerminalsActive(t)
	f := newFakeTermsvc()
	f.snap = []dashapi.LaunchInfo{{ID: "h-1", RunID: "-XJMmI9f6HCyVmfLBjmO28n2WWx8G6UQRv-NFQ6Tf30", Tool: "codex", Kind: "fresh", CreatedAt: f.now}}
	serveFakeDaemon(t, f)

	_, errb, code := runVerb(t, "terminals", "show", "-XJMmI9f6HCyVmfLBjmO28n2WWx8G6UQRv-NFQ6Tf30")
	if code == 0 || !strings.Contains(errb, "unknown shorthand flag") || !strings.Contains(errb, "goes after --: observer terminals show -- <id>") {
		t.Fatalf("show without -- = %d %q", code, errb)
	}
	out, errb, code := runVerb(t, "terminals", "show", "--json", "--", "-XJMmI9f6HCyVmfLBjmO28n2WWx8G6UQRv-NFQ6Tf30")
	var run struct {
		RunID string `json:"run_id"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &run) != nil || run.RunID != "-XJMmI9f6HCyVmfLBjmO28n2WWx8G6UQRv-NFQ6Tf30" {
		t.Fatalf("show after -- = %d %q %q", code, out, errb)
	}
	if out, errb, code := runVerb(t, "terminals", "stop", "--", "-XJMmI9f6HCy"); code != 0 || f.stopped["h-1"] != "cli_stop" {
		t.Fatalf("stop after -- = %d %q %q %v", code, out, errb, f.stopped)
	}
	// confirm says the same.
	_, errb, code = runVerb(t, "terminals", "confirm", "-Xpend")
	if code == 0 || !strings.Contains(errb, "observer terminals confirm -- <id>") {
		t.Fatalf("confirm without -- = %d %q", code, errb)
	}
}
