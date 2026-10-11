package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marmutapp/superbased-observer/internal/guardposture"
	"github.com/marmutapp/superbased-observer/internal/integration"
)

// TestRecordUnclassifiedToolGate pins review M2-12's cmd half: an
// unclassified wired gate call is spooled once per window per (tool, event,
// native tool), as a non-enforced record naming the tool, and an ungated
// plan records nothing.
func TestRecordUnclassifiedToolGate(t *testing.T) {
	spool := filepath.Join(t.TempDir(), guardposture.SpoolFilename)
	plan := hookFailurePlan{Tool: "codex", Event: "PreToolUse", Cap: integration.HookCapToolGate, Gated: true, SpoolPath: spool}
	var errb bytes.Buffer
	recordUnclassifiedToolGate(plan, "TotallyUnknownTool9", "s1", &errb)
	recordUnclassifiedToolGate(plan, "TotallyUnknownTool9", "s1", &errb)
	recordUnclassifiedToolGate(plan, "OtherUnknown", "s1", &errb)
	ungated := plan
	ungated.Gated = false
	recordUnclassifiedToolGate(ungated, "Third", "s1", &errb)
	if errb.Len() != 0 {
		t.Fatalf("stderr = %q", errb.String())
	}
	b, err := guardposture.Drain(spool, 0)
	if err != nil || b == nil {
		t.Fatalf("drain = %v, %v", b, err)
	}
	if len(b.Records) != 2 {
		t.Fatalf("records = %+v, want one per native tool inside the window", b.Records)
	}
	r := b.Records[0]
	if r.Kind != guardposture.KindUnclassified || r.Native != "TotallyUnknownTool9" || !r.FailedOpen() || r.SessionID != "s1" {
		t.Fatalf("record = %+v", r)
	}
	v := hookFailureVerdict(r)
	if v.Enforced || !strings.Contains(v.Verdict.Reason, "TotallyUnknownTool9") || !strings.Contains(v.Verdict.Reason, "unjudged") {
		t.Fatalf("verdict = %+v", v)
	}
}
