package guard

import (
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/config"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/policy"
	"github.com/marmutapp/superbased-observer/internal/scrub"
)

// fakeFinePAT is an obviously synthetic fine-grained-PAT-shaped value,
// assembled so no literal in this file is itself credential-shaped.
var fakeFinePAT = "github_pat_11" + strings.Repeat("AbC1_", 6)

func capturedPrompt(text string) ActionInput {
	return ActionInput{
		ActionID:    331,
		SessionID:   "b9cba337-test",
		ProjectRoot: "/home/u/proj",
		Tool:        "antigravity-cli",
		ActionType:  models.ActionUserPrompt,
		Target:      "excerpt only",
		PromptText:  text,
		Timestamp:   time.Date(2026, 10, 2, 13, 45, 22, 0, time.UTC),
	}
}

// TestPostHocPromptLane is the table for the watcher-path prompt lane
// (posthocprompt.go): one row per decision the lane makes.
func TestPostHocPromptLane(t *testing.T) {
	cases := []struct {
		name       string
		cfg        func() config.GuardConfig
		in         func() ActionInput
		preSent    bool
		wantRecord bool
		wantRule   string
		wantType   string
	}{
		{
			name:       "raw fine-grained PAT the storage scrubber missed",
			cfg:        func() config.GuardConfig { return promptCfg("observe", "ask-once", nil) },
			in:         func() ActionInput { return capturedPrompt("please push with " + fakeFinePAT + " thanks") },
			wantRecord: true, wantRule: "R-172", wantType: "github_pat_fine",
		},
		{
			name:       "typed storage marker is a certain finding",
			cfg:        func() config.GuardConfig { return promptCfg("observe", "ask-once", nil) },
			in:         func() ActionInput { return capturedPrompt("please push with [REDACTED:github_pat_fine] thanks") },
			wantRecord: true, wantRule: "R-172", wantType: "github_pat_fine",
		},
		{
			// Audit P4: the older full-match storage rows now write typed
			// markers too, so a classic token in a no-seam tool is flagged
			// from its stored (already redacted) form.
			name: "classic PAT through the real storage scrubber",
			cfg:  func() config.GuardConfig { return promptCfg("observe", "ask-once", nil) },
			in: func() ActionInput {
				return capturedPrompt(scrub.New().String("push with ghp_" + strings.Repeat("Ab1", 8) + " thanks"))
			},
			wantRecord: true, wantRule: "R-172", wantType: "github_pat",
		},
		{
			name: "AWS key id through the real storage scrubber",
			cfg:  func() config.GuardConfig { return promptCfg("observe", "ask-once", nil) },
			in: func() ActionInput {
				return capturedPrompt(scrub.New().String("use AKIA" + "ABCDEFGHIJKLMNOP" + " for s3"))
			},
			wantRecord: true, wantRule: "R-172", wantType: "aws_access_key",
		},
		{
			name: "bare marker from prose is not evidence",
			cfg:  func() config.GuardConfig { return promptCfg("observe", "ask-once", nil) },
			in:   func() ActionInput { return capturedPrompt("add [REDACTED] auth to the client") },
		},
		{
			name: "assignment around a removed value is not a finding",
			cfg:  func() config.GuardConfig { return promptCfg("observe", "ask-once", nil) },
			in:   func() ActionInput { return capturedPrompt("set token: [REDACTED] in the env") },
		},
		{
			name:       "falls back to the target excerpt when no raw input was stored",
			cfg:        func() config.GuardConfig { return promptCfg("observe", "ask-once", nil) },
			in:         func() ActionInput { in := capturedPrompt(""); in.Target = "key " + fakeFinePAT; return in },
			wantRecord: true, wantRule: "R-172", wantType: "github_pat_fine",
		},
		{
			name:    "a pre-send lane already judged this prompt",
			cfg:     func() config.GuardConfig { return promptCfg("observe", "ask-once", nil) },
			in:      func() ActionInput { return capturedPrompt("key " + fakeFinePAT) },
			preSent: true,
		},
		{
			name: "prompt lane disabled",
			cfg: func() config.GuardConfig {
				c := promptCfg("observe", "ask-once", nil)
				c.Prompt.Enabled = false
				return c
			},
			in: func() ActionInput { return capturedPrompt("key " + fakeFinePAT) },
		},
		{
			name: "global prompt mode off",
			cfg:  func() config.GuardConfig { return promptCfg("observe", "off", nil) },
			in:   func() ActionInput { return capturedPrompt("key " + fakeFinePAT) },
		},
		{
			name: "detector exempted per-detector stays off for markers too",
			cfg: func() config.GuardConfig {
				return promptCfg("observe", "ask-once", map[string]string{"github_pat_fine": "off"})
			},
			in: func() ActionInput { return capturedPrompt("key [REDACTED:github_pat_fine]") },
		},
		{
			name:       "enforce + block still only records a flag after the fact",
			cfg:        func() config.GuardConfig { return promptCfg("enforce", "block", nil) },
			in:         func() ActionInput { return capturedPrompt("key " + fakeFinePAT) },
			wantRecord: true, wantRule: "R-172", wantType: "github_pat_fine",
		},
		{
			name: "clean prompt",
			cfg:  func() config.GuardConfig { return promptCfg("observe", "ask-once", nil) },
			in:   func() ActionInput { return capturedPrompt("refactor the parser please") },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newTestGuard(t, tc.cfg(), nil)
			var lookups int
			g.SetPromptPreSendLookup(func(sessionID, digest string, at time.Time) bool {
				lookups++
				if sessionID != "b9cba337-test" || at.IsZero() || len(digest) != 64 {
					t.Errorf("lookup got session=%q digest=%q at=%v", sessionID, digest, at)
				}
				return tc.preSent
			})
			in := tc.in()
			out := g.EvaluateActions([]ActionInput{in})
			if !tc.wantRecord {
				if len(out) != 0 {
					t.Fatalf("want no verdict, got %+v", out)
				}
				return
			}
			if len(out) != 1 {
				t.Fatalf("want 1 verdict, got %d: %+v", len(out), out)
			}
			av := out[0]
			if av.Verdict.RuleID != tc.wantRule {
				t.Errorf("rule = %q, want %q", av.Verdict.RuleID, tc.wantRule)
			}
			if av.Kind != policy.KindUserPrompt {
				t.Errorf("kind = %q, want user_prompt", av.Kind)
			}
			if av.Verdict.Decision != policy.DecisionFlag || av.Enforced {
				t.Errorf("post-hoc verdict must be an unenforced flag: decision=%v enforced=%v", av.Verdict.Decision, av.Enforced)
			}
			if !strings.Contains(av.Verdict.Reason, "detected "+tc.wantType) {
				t.Errorf("reason %q does not name %s", av.Verdict.Reason, tc.wantType)
			}
			if !strings.Contains(av.Verdict.Reason, "[outcome=detected]") || !strings.Contains(av.Verdict.Reason, "already sent") {
				t.Errorf("reason %q lacks the post-hoc outcome/explanation", av.Verdict.Reason)
			}
			if av.SuppressAlert {
				t.Error("post-hoc verdict must not suppress the desktop alert: it is the only signal the developer gets")
			}
			if av.Input.ActionID != in.ActionID || av.Input.Tool != in.Tool || av.Input.SessionID != in.SessionID {
				t.Errorf("anchor lost: %+v", av.Input)
			}
			if av.Input.PromptText != "" {
				t.Error("PromptText survived into the verdict")
			}
			if len(av.Input.Target) != 64 {
				t.Errorf("target must be the 64-hex fingerprint, got %q", av.Input.Target)
			}
			for _, field := range []string{av.Input.Target, av.Verdict.Reason, av.Verdict.Advice, av.TaintOrigin} {
				if strings.Contains(field, "github_pat_11") || strings.Contains(field, "AbC1_") {
					t.Fatalf("matched value leaked into the verdict: %q", field)
				}
			}
			if lookups != 1 {
				t.Errorf("pre-send lookup ran %d times, want 1", lookups)
			}
		})
	}
}

// TestPostHocPromptLane_LookupOnlyOnFindings pins that the store query
// behind the dedupe lookup runs only for a prompt that actually carries a
// finding — the common clean prompt costs no read.
func TestPostHocPromptLane_LookupOnlyOnFindings(t *testing.T) {
	g := newTestGuard(t, promptCfg("observe", "ask-once", nil), nil)
	g.SetPromptPreSendLookup(func(string, string, time.Time) bool {
		t.Fatal("lookup must not run for a clean prompt")
		return false
	})
	if out := g.EvaluateActions([]ActionInput{capturedPrompt("hello there")}); len(out) != 0 {
		t.Fatalf("clean prompt produced %+v", out)
	}
}

// TestPostHocPromptLane_NoLookupWired records when no lookup is wired
// (the documented nil default).
func TestPostHocPromptLane_NoLookupWired(t *testing.T) {
	g := newTestGuard(t, promptCfg("observe", "ask-once", nil), nil)
	out := g.EvaluateActions([]ActionInput{capturedPrompt("key " + scrub.TypedRedactedMarker("slack_token"))})
	if len(out) != 1 || out[0].Verdict.RuleID != "R-172" {
		t.Fatalf("want one R-172 flag with no lookup wired, got %+v", out)
	}
}

// TestPostHocPromptLane_StorageScrubRoundTrip drives the realistic path
// end to end: the adapter's storage scrub runs first, then the watcher
// lane must still name the detector.
func TestPostHocPromptLane_StorageScrubRoundTrip(t *testing.T) {
	stored := scrub.New().String("use this pat " + fakeFinePAT + " to push")
	if strings.Contains(stored, fakeFinePAT) {
		t.Fatalf("storage scrub left the token: %q", stored)
	}
	g := newTestGuard(t, promptCfg("observe", "ask-once", nil), nil)
	out := g.EvaluateActions([]ActionInput{capturedPrompt(stored)})
	if len(out) != 1 || !strings.Contains(out[0].Verdict.Reason, "detected github_pat_fine") {
		t.Fatalf("want a github_pat_fine flag after storage scrubbing, got %+v", out)
	}
}

// TestPostHocPromptLane_DedupesOnlyTheSamePrompt is the per-prompt dedupe
// table: a pre-send mark covers exactly the prompt it was computed from
// (raw on the pre-send side, scrubbed on the capture side), never a
// different secret-bearing prompt in the same session.
func TestPostHocPromptLane_DedupesOnlyTheSamePrompt(t *testing.T) {
	first := "push with " + fakeFinePAT + " now"
	second := "and also this one " + "github_pat_22" + strings.Repeat("ZzY8_", 6)
	marked := map[string]bool{PromptCaptureDigest(first): true}
	cases := []struct {
		name       string
		stored     string
		wantRecord bool
	}{
		{"same prompt (stored scrubbed) is deduped", scrub.New().String(first), false},
		{"same prompt (storage missed the value) is deduped", first, false},
		{"same prompt with different whitespace is deduped", "push  with	" + fakeFinePAT + "   now", false},
		{"a different secret prompt in the window is still flagged", scrub.New().String(second), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newTestGuard(t, promptCfg("observe", "ask-once", nil), nil)
			g.SetPromptPreSendLookup(func(_ string, digest string, _ time.Time) bool { return marked[digest] })
			out := g.EvaluateActions([]ActionInput{capturedPrompt(tc.stored)})
			if (len(out) == 1) != tc.wantRecord {
				t.Fatalf("recorded %d verdicts, want record=%v", len(out), tc.wantRecord)
			}
		})
	}
}

func TestPromptPreSendDigest(t *testing.T) {
	fixture := "token " + fakeFinePAT
	cases := []struct {
		name         string
		text         string
		recordWorthy bool
		wantMark     bool
	}{
		{"record-worthy always marks", "card 4532015112830366", true, true},
		{"allowed prompt that storage will type-redact marks", fixture, false, true},
		{"allowed plain prompt does not mark", "refactor the parser", false, false},
	}
	for _, tc := range cases {
		got := PromptPreSendDigest(tc.text, tc.recordWorthy)
		if (got != "") != tc.wantMark {
			t.Errorf("%s: PromptPreSendDigest = %q, want mark=%v", tc.name, got, tc.wantMark)
		}
		if got != "" && got != PromptCaptureDigest(tc.text) {
			t.Errorf("%s: mark is not the capture digest", tc.name)
		}
	}
	if PromptCaptureDigest("   ") != "" {
		t.Error("an empty prompt must have no digest")
	}
}
