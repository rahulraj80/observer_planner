package hook

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marmutapp/superbased-observer/internal/guard"
	"github.com/marmutapp/superbased-observer/internal/models"
	"github.com/marmutapp/superbased-observer/internal/policy"
)

// agyTranscript writes a transcript.jsonl shaped like the one grounded on
// demo node 1 (a USER_INPUT step wrapped in <USER_REQUEST>, then model
// steps) and returns its path.
func agyTranscript(t *testing.T, prompts ...string) string {
	t.Helper()
	return agyTranscriptNamed(t, "transcript.jsonl", prompts...)
}

// agyTranscriptNamed is agyTranscript with the file name agy chose: agy
// 1.2.16 wrote transcript.jsonl, agy 1.3.0 points PreInvocation at
// transcript_full.jsonl (demo node 1, 2026-10-06, T-476).
func agyTranscriptNamed(t *testing.T, name string, prompts ...string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "brain", "conv-1", ".system_generated", "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i, p := range prompts {
		user, _ := json.Marshal(map[string]any{"step_index": i * 2, "source": "USER_EXPLICIT", "type": "USER_INPUT", "status": "DONE", "content": "<USER_REQUEST>\n" + p + "\n</USER_REQUEST>"})
		model, _ := json.Marshal(map[string]any{"step_index": i*2 + 1, "source": "MODEL", "type": "PLANNER_RESPONSE", "status": "DONE"})
		b.Write(user)
		b.WriteByte('\n')
		b.Write(model)
		b.WriteByte('\n')
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func agyPayload(t *testing.T, transcript string) []byte {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"invocationNum": 0, "conversationId": "conv-1", "transcriptPath": transcript, "modelName": "gemini-3.6-flash"})
	return body
}

func TestExtractAntigravityPreInvocationPrompt(t *testing.T) {
	t.Parallel()
	path := agyTranscript(t, "first prompt", "push with the token please")
	prompt, sid, ok, missing := extractAntigravityPreInvocationPrompt(agyPayload(t, path))
	if !ok || missing || sid != "conv-1" || prompt != "push with the token please" {
		t.Fatalf("got (%q, %q, ok=%v, missing=%v), want the LATEST user input unwrapped", prompt, sid, ok, missing)
	}
	full := agyTranscriptNamed(t, "transcript_full.jsonl", "a prompt agy 1.3.0 wrote")
	if prompt, _, ok, _ := extractAntigravityPreInvocationPrompt(agyPayload(t, full)); !ok || prompt != "a prompt agy 1.3.0 wrote" {
		t.Fatalf("transcript_full.jsonl: got (%q, ok=%v), want the agy 1.3.0 prompt", prompt, ok)
	}
	cases := map[string]string{
		"no transcript path":  "",
		"not transcript.json": filepath.Join(filepath.Dir(path), "other.jsonl"),
		"missing file":        filepath.Join(t.TempDir(), "transcript.jsonl"),
	}
	for name, p := range cases {
		if _, _, ok, _ := extractAntigravityPreInvocationPrompt(agyPayload(t, p)); ok {
			t.Errorf("%s: want ok=false (no scan, capture lane covers it)", name)
		}
	}
}

// judgedEvaluator is a stubPromptEvaluator that also answers
// PromptAlreadyJudged.
type judgedEvaluator struct {
	stubPromptEvaluator
	judged bool
	calls  int
}

func (j *judgedEvaluator) PromptAlreadyJudged(string, string, time.Time) bool {
	j.calls++
	return j.judged
}

// TestHandlePromptSubmitGuarded_AntigravityPreInvocation pins the agy
// seam: a finding warns the MODEL through injectSteps (warn-only, never
// stops), the developer toast stays on (WarnIndirect), a prompt already
// judged is skipped, and a clean prompt replies {} with no DB lookup.
func TestHandlePromptSubmitGuarded_AntigravityPreInvocation(t *testing.T) {
	t.Parallel()
	path := agyTranscript(t, "push with the token please")
	finding := []policy.SecretFinding{{Type: "github_pat_fine", Hash: "h"}}
	ask := guard.PromptVerdict{
		Verdict: policy.Verdict{Decision: policy.DecisionAsk, RuleID: "R-172", Reason: "detected github_pat_fine in the prompt text"},
		Outcome: guard.PromptOutcomeBlocked,
	}
	cases := []struct {
		name        string
		ev          *judgedEvaluator
		wantInject  bool
		wantPersist bool
		wantLookups int
	}{
		{"finding warns the model", &judgedEvaluator{stubPromptEvaluator: stubPromptEvaluator{secrets: finding, verdict: ask}}, true, true, 1},
		{"already judged: skipped", &judgedEvaluator{stubPromptEvaluator: stubPromptEvaluator{secrets: finding, verdict: ask}, judged: true}, false, false, 1},
		{"clean prompt: no lookup", &judgedEvaluator{stubPromptEvaluator: stubPromptEvaluator{verdict: allowVerdict()}}, false, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var got guard.PromptVerdict
			persisted := false
			handled, after, exit := HandlePromptSubmitGuarded(models.ToolAntigravityCLI, PromptDialectAntigravityPreInvocation, "PreInvocation",
				agyPayload(t, path), false, tc.ev,
				func(pv guard.PromptVerdict, _ guard.Emission, _ string) { got, persisted = pv, true }, &stdout, io.Discard)
			if !handled || exit != 0 {
				t.Fatalf("handled=%v exit=%d, want handled with exit 0 (the seam never stops)", handled, exit)
			}
			if after != nil {
				after()
			}
			if persisted != tc.wantPersist || tc.ev.calls != tc.wantLookups {
				t.Fatalf("persisted=%v lookups=%d, want %v/%d", persisted, tc.ev.calls, tc.wantPersist, tc.wantLookups)
			}
			hasInject := strings.Contains(stdout.String(), `"injectSteps"`)
			if hasInject != tc.wantInject {
				t.Fatalf("stdout = %q, want injectSteps=%v", stdout.String(), tc.wantInject)
			}
			if !tc.wantPersist {
				return
			}
			if got.Action != guard.PromptActionWarn || got.ActionDegradedFrom != guard.PromptActionConfirm || got.Lane != "hook" || !got.WarnIndirect {
				t.Errorf("verdict = action %q wanted %q lane %q indirect %v, want warn (degraded from confirm), indirect",
					got.Action, got.ActionDegradedFrom, got.Lane, got.WarnIndirect)
			}
			if !strings.Contains(stdout.String(), "SuperBased Observer guard notice") || strings.Contains(stdout.String(), "github_pat_11") {
				t.Errorf("model notice = %q", stdout.String())
			}
		})
	}
}

func TestRegisterAntigravityCLI(t *testing.T) {
	t.Parallel()
	r := genericSettingsRegistry(t, filepath.Join(".gemini", "config"))
	path := filepath.Join(r.opts.HomeDir, ".gemini", "config", "hooks.json")
	// A foreign hook name plus a stale observer PreInvocation entry an
	// earlier version wrote (timeout 10): it is refreshed to the current
	// entry (T-476 restored PreInvocation on agy 1.3.0).
	seed := `{"reminder":{"PreInvocation":[{"type":"command","command":"./r.sh"}]},` +
		`"superbased-observer":{"enabled":true,"PreInvocation":[{"type":"command","command":"/opt/observer/bin/observer hook antigravity-cli PreInvocation","timeout":10}]}}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	res := r.Register("antigravity-cli")
	if res.Error != nil || strings.Join(res.HooksAdded, ",") != "PreToolUse,PreInvocation" {
		t.Fatalf("Register = %+v", res)
	}
	raw, _ := os.ReadFile(path)
	var top map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("hooks.json not valid: %v\n%s", err, raw)
	}
	mine := top[agyObserverHookName]
	var pre []agyHookHandler
	if err := json.Unmarshal(mine["PreInvocation"], &pre); err != nil || len(pre) != 1 ||
		pre[0].Command != "/opt/observer/bin/observer hook antigravity-cli PreInvocation" ||
		pre[0].Timeout != agyToolGateTimeoutSeconds {
		t.Fatalf("PreInvocation entry not refreshed: %v\n%s", err, raw)
	}
	var groups []agyHookGroup
	if err := json.Unmarshal(mine["PreToolUse"], &groups); err != nil {
		t.Fatalf("PreToolUse not matcher groups: %v\n%s", err, raw)
	}
	want := "/opt/observer/bin/observer hook antigravity-cli PreToolUse"
	if string(mine["enabled"]) != "true" || len(groups) != 1 || groups[0].Matcher != "*" ||
		len(groups[0].Hooks) != 1 || groups[0].Hooks[0].Command != want ||
		groups[0].Hooks[0].Timeout != agyToolGateTimeoutSeconds {
		t.Fatalf("observer hook = %s", raw)
	}
	if !strings.Contains(string(raw), `"./r.sh"`) {
		t.Fatalf("foreign hook name not preserved: %s", raw)
	}
	if again := r.Register("antigravity-cli"); again.Error != nil || len(again.AlreadySet) != 2 {
		t.Fatalf("re-register = %+v, want AlreadySet", again)
	}
	un := r.Unregister("antigravity-cli")
	if un.Error != nil || len(un.HooksRemoved) != 2 {
		t.Fatalf("Unregister = %+v", un)
	}
	raw, _ = os.ReadFile(path)
	if strings.Contains(string(raw), agyObserverHookName) || !strings.Contains(string(raw), "./r.sh") {
		t.Fatalf("after unregister: %s", raw)
	}
}

// TestInstalledDetectsAntigravityCLI pins the auto-register signal for the
// agy hook (demo node 1, 2026-10-03: it was never written
// because Installed() did not list the tool).
func TestInstalledDetectsAntigravityCLI(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".gemini", "antigravity-cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry(Options{BinaryPath: "/x", HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range r.Installed() {
		if tool == "antigravity-cli" {
			return
		}
	}
	t.Errorf("Installed()=%v does not include antigravity-cli", r.Installed())
}
