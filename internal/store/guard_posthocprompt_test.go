package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/policy"
	"github.com/marmutapp/superbased-observer/internal/scrub"
)

var storeFakeFinePAT = "github_pat_11" + strings.Repeat("XyZ9_", 6)

// newPromptGuardedStore is newGuardedStore with [guard.prompt] on (the
// production default) and the pre-send dedupe lookup wired the way
// cmd/observer/guardwire.go wires it.
func newPromptGuardedStore(t *testing.T) *Store {
	t.Helper()
	s, _ := newTestStore(t)
	g, err := guard.New(guard.Options{
		Config: config.GuardConfig{
			Enabled: true, Mode: "observe",
			Taint: config.GuardTaintConfig{Enabled: true, DecayTurns: 10},
			Prompt: config.GuardPromptConfig{
				Enabled: true, Mode: "ask-once", HookLane: true, ProxyLane: true,
				EnforceIndependent: true, ReconsiderTTL: "30m", MaxFindings: 64,
			},
		},
		Home:     "/home/u",
		ReadFile: func(string) ([]byte, error) { return nil, os.ErrNotExist },
	})
	if err != nil {
		t.Fatalf("guard.New: %v", err)
	}
	g.SetPromptPreSendLookup(func(sessionID, digest string, at time.Time) bool {
		ok, err := s.PreSendPromptJudged(context.Background(), sessionID, digest, at)
		if err != nil {
			t.Errorf("PreSendPromptJudged: %v", err)
		}
		return ok
	})
	s.SetGuard(g)
	return s
}

func postHocPromptEvent(session, id, raw string, ts time.Time) models.ToolEvent {
	return models.ToolEvent{
		SourceFile: "transcript.jsonl", SourceEventID: id, SessionID: session, ProjectRoot: "/home/u/proj",
		Timestamp: ts, Tool: "antigravity-cli", ActionType: models.ActionUserPrompt,
		Target: raw, RawToolInput: raw, Success: true,
	}
}

// TestIngest_PostHocPromptLane is the end-to-end pin for the 2026-10-02
// demo finding: a prompt captured from a tool with no pre-send seam,
// after the adapter's storage scrub, lands ONE R-172 flag anchored to its
// action with a content-free target; re-ingest adds nothing.
func TestIngest_PostHocPromptLane(t *testing.T) {
	t.Parallel()
	s := newPromptGuardedStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 2, 13, 45, 22, 0, time.UTC)

	stored := scrub.New().String("push the branch using " + storeFakeFinePAT + " please")
	events := []models.ToolEvent{
		postHocPromptEvent("sAgy", "u1", stored, now),
		postHocPromptEvent("sAgy", "u2", "now refactor the parser", now.Add(time.Minute)),
	}
	res, err := s.Ingest(ctx, events, nil, IngestOptions{})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.GuardEventsRecorded != 1 {
		t.Fatalf("GuardEventsRecorded = %d, want 1", res.GuardEventsRecorded)
	}
	rows, err := s.LoadGuardEventsForSession(ctx, "sAgy")
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %+v (err %v), want 1", rows, err)
	}
	row := rows[0]
	if row.RuleID != "R-172" || row.EventKind != "user_prompt" || row.Decision != "flag" || row.Enforced {
		t.Errorf("row = %+v, want an unenforced R-172 user_prompt flag", row)
	}
	if row.ActionID == nil {
		t.Error("post-hoc prompt row must anchor to its action (that is what separates it from a pre-send row)")
	}
	if row.Tool != "antigravity-cli" {
		t.Errorf("tool = %q", row.Tool)
	}
	if !strings.Contains(row.Reason, "github_pat_fine") || !strings.Contains(row.Reason, "[outcome=detected]") {
		t.Errorf("reason = %q", row.Reason)
	}
	for _, f := range []string{row.Reason, row.TargetExcerpt} {
		if strings.Contains(f, "XyZ9_") || strings.Contains(f, "push the branch") {
			t.Fatalf("prompt content leaked into guard_events: %q", f)
		}
	}
	if len(row.TargetExcerpt) != 64 {
		t.Errorf("target_excerpt must be the fingerprint, got %q", row.TargetExcerpt)
	}

	res, err = s.Ingest(ctx, events, nil, IngestOptions{})
	if err != nil {
		t.Fatalf("re-Ingest: %v", err)
	}
	if res.GuardEventsRecorded != 0 {
		t.Errorf("re-ingest recorded %d, want 0", res.GuardEventsRecorded)
	}
	if report, err := s.VerifyGuardChain(ctx); err != nil || !report.OK {
		t.Errorf("chain = (%+v, %v), want OK", report, err)
	}
}

// TestIngest_PostHocPromptLane_DefersToPreSendLane pins the per-prompt
// dedupe end to end: a hook-lane verdict persisted through the one writer
// leaves a mark for ITS prompt; the same prompt captured later is not
// recorded again, while a DIFFERENT secret-bearing prompt in the same
// session inside the window, and the same prompt resent outside the
// window, both are.
func TestIngest_PostHocPromptLane_DefersToPreSendLane(t *testing.T) {
	t.Parallel()
	s := newPromptGuardedStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 2, 11, 4, 36, 0, time.UTC)

	rawFirst := "please push using " + storeFakeFinePAT
	if _, err := s.PersistGuardVerdicts(ctx, []guard.ActionVerdict{{
		Input: guard.ActionInput{
			SessionID: "sCC", Tool: "claude-code", Target: strings.Repeat("a", 64), Timestamp: now,
			PromptDigest: guard.PromptCaptureDigest(rawFirst),
		},
		Kind: policy.KindUserPrompt, Category: "exfil",
		Verdict: policy.Verdict{
			Decision: policy.DecisionAsk, RuleID: "R-172", Severity: policy.SeverityCritical,
			Reason: "detected github_pat_fine in the prompt text [outcome=blocked]", Source: "builtin",
		},
		Enforced: true,
	}}); err != nil {
		t.Fatalf("seed pre-send row: %v", err)
	}

	sc := scrub.New()
	same := postHocPromptEvent("sCC", "c1", sc.String(rawFirst), now.Add(40*time.Second))
	other := postHocPromptEvent("sCC", "c2", sc.String("second key "+"github_pat_22"+strings.Repeat("QwE7_", 6)), now.Add(2*time.Minute))
	later := postHocPromptEvent("sCC", "c3", sc.String(rawFirst), now.Add(2*time.Hour))
	for _, e := range []*models.ToolEvent{&same, &other, &later} {
		e.Tool = "claude-code"
	}
	res, err := s.Ingest(ctx, []models.ToolEvent{same, other, later}, nil, IngestOptions{})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.GuardEventsRecorded != 2 {
		t.Fatalf("GuardEventsRecorded = %d, want 2 (the different prompt and the late resend)", res.GuardEventsRecorded)
	}
	rows, _ := s.LoadGuardEventsForSession(ctx, "sCC")
	got := map[time.Time]bool{}
	for _, r := range rows {
		if r.ActionID != nil {
			got[r.TS] = true
		}
	}
	if got[now.Add(40*time.Second)] || !got[now.Add(2*time.Minute)] || !got[now.Add(2*time.Hour)] {
		t.Errorf("capture rows at %v, want the different prompt and the late resend only", got)
	}
}

// TestPersistGuardVerdicts_MarkOnlyPromptVerdict pins the mark-only shape
// the hook lane persists for an allowed prompt (an allow-listed fixture):
// a mark, and NO guard_events row.
func TestPersistGuardVerdicts_MarkOnlyPromptVerdict(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	n, err := s.PersistGuardVerdicts(ctx, []guard.ActionVerdict{{
		Input:   guard.ActionInput{SessionID: "sM", Target: "", Timestamp: now, PromptDigest: "d1"},
		Kind:    policy.KindUserPrompt,
		Verdict: policy.Verdict{Decision: policy.DecisionAllow},
	}})
	if err != nil || n != 0 {
		t.Fatalf("PersistGuardVerdicts = (%d, %v), want (0, nil)", n, err)
	}
	if rows, _ := s.LoadGuardEventsForSession(ctx, "sM"); len(rows) != 0 {
		t.Fatalf("mark-only verdict wrote guard_events rows: %+v", rows)
	}
	if ok, err := s.PreSendPromptJudged(ctx, "sM", "d1", now); err != nil || !ok {
		t.Fatalf("PreSendPromptJudged = (%v, %v), want (true, nil)", ok, err)
	}
}

func TestPreSendPromptJudged(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 2, 14, 10, 9, 715728546, time.UTC)
	if err := s.RecordPromptPreSend(ctx, "s1", "dA", at); err != nil {
		t.Fatalf("RecordPromptPreSend: %v", err)
	}
	if err := s.RecordPromptPreSend(ctx, "", "dA", at); err != nil {
		t.Fatalf("empty session must be a no-op, got %v", err)
	}
	cases := []struct {
		name            string
		session, digest string
		at              time.Time
		want            bool
	}{
		{"same prompt, same instant", "s1", "dA", at, true},
		{"second-precision capture just before", "s1", "dA", at.Truncate(time.Second), true},
		{"inside window after", "s1", "dA", at.Add(29 * time.Minute), true},
		{"outside window", "s1", "dA", at.Add(31 * time.Minute), false},
		{"different prompt, same session", "s1", "dB", at, false},
		{"other session", "s2", "dA", at, false},
		{"empty digest", "s1", "", at, false},
	}
	for _, tc := range cases {
		got, err := s.PreSendPromptJudged(ctx, tc.session, tc.digest, tc.at)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
	// Retention: a mark written a week later prunes the old one.
	if err := s.RecordPromptPreSend(ctx, "s1", "dC", at.Add(8*24*time.Hour)); err != nil {
		t.Fatalf("RecordPromptPreSend: %v", err)
	}
	if ok, _ := s.PreSendPromptJudged(ctx, "s1", "dA", at); ok {
		t.Error("mark older than the retention survived a later write")
	}
}

// TestLoadGuardReplayInputs_PromptText pins that the rescan/simulate
// replay hands the post-hoc lane the full stored prompt for user_prompt
// rows and nothing extra for other action types.
func TestLoadGuardReplayInputs_PromptText(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	long := strings.Repeat("context ", 40) + scrub.TypedRedactedMarker("github_pat_fine")
	cmd := postHocPromptEvent("sR", "r2", "ls -la", now.Add(time.Second))
	cmd.ActionType = models.ActionRunCommand
	cmd.RawToolInput = `{"command":"ls -la"}`
	if _, err := s.Ingest(ctx, []models.ToolEvent{postHocPromptEvent("sR", "r1", long, now), cmd}, nil, IngestOptions{}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	inputs, err := s.LoadGuardReplayInputs(ctx, now.Add(-time.Hour), 0)
	if err != nil {
		t.Fatalf("LoadGuardReplayInputs: %v", err)
	}
	seen := map[string]string{}
	for _, in := range inputs {
		seen[in.ActionType] = in.PromptText
	}
	if seen[models.ActionUserPrompt] != long {
		t.Errorf("user_prompt PromptText = %q, want the full stored prompt", seen[models.ActionUserPrompt])
	}
	if seen[models.ActionRunCommand] != "" {
		t.Errorf("run_command PromptText = %q, want empty", seen[models.ActionRunCommand])
	}
}
