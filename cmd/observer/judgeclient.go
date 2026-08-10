//go:build !no_obs

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/llmconn"
)

// chatCompletionsJudge calls an OpenAI-compatible /chat/completions endpoint
// (OpenRouter by default) for a single LLM-as-judge turn and returns the reply
// text. It is the host's implementation detail behind the obs eval.JudgeClient
// (wired in obs_wire.go) — kept in its own file with NO internal/obs import so
// it stays generic and the reverse-import boundary holds (only obs_wire.go may
// import obs). This is the SINGLE outbound network call in the whole eval
// subsystem; it runs only for an explicitly-invoked `observer eval run`.
//
// The judge now rides the SHARED internal/llmconn client (standup-briefing
// plan §3 / §15 Q6 — migrate judgeclient first): the HTTP verbs, retries,
// dialect bodies, response parsing, and egress guardrails all live in one
// place, and the turn is tagged with the shared analysis attribution
// (X-SBO-Analysis + X-Title "superbased-observer analysis") so judge spend is
// distinguishable from tool traffic on the provider dashboard. A per-origin
// attribution marker is the documented Q4 follow-up.
//
// hv is the Authorization header value's credential portion, sourced from an
// env var by the caller (never written to config or disk).
type chatCompletionsJudge struct {
	baseURL string
	hv      string
	// scrubPrompt, when set, redacts secrets from the prompt BEFORE it is
	// sent upstream so raw secrets never egress to a hosted judge (admission
	// spec §5). Set only for a REMOTE judge (hosting != local) at the wiring
	// point; nil for a loopback judge (no egress, no overhead). Injected as a
	// func so this generic client keeps no internal/scrub import.
	scrubPrompt func(string) string
	// maxPromptBytes caps the prompt sent upstream (0 = uncapped) — a payload
	// ceiling for a remote judge (admission spec §5).
	maxPromptBytes int
	// maxTokens caps the judge REPLY length (0 = omit the field, provider
	// default). The reply is only a short JSON verdict, so a tight cap bounds
	// latency + cost. Defaulted by newChatCompletionsJudge; overridable from
	// config via withTuning.
	maxTokens int
	// numCtx, when >0 AND the judge is loopback-local, is passed through as an
	// Ollama-style context-window hint. It is NEVER sent to a remote judge (a
	// hosted OpenAI-compatible API rejects unknown fields). 0 = omit.
	numCtx int
	// local marks a loopback/no-egress judge (Ollama-style). When true the
	// credential may be empty (a local host needs no API key) and an
	// Ollama-style num_ctx hint may be passed through. Derived from the base
	// URL at construction — hosting is URL-derived throughout this codebase
	// (there is no hosting= config field; judgeHostingLabel uses the same test).
	local bool
}

const (
	// defaultJudgeMaxTokens caps the judge reply by default — a {"score",
	// "rationale"} verdict fits comfortably; this stops a chatty model from
	// streaming an unbounded reply and bounds hosted-judge cost.
	defaultJudgeMaxTokens = 512
	// judgeSystemPrompt pins the judge's reply contract on every turn. The
	// shared llmconn client carries it as the system message.
	judgeSystemPrompt = "You are a strict evaluation judge. Reply with a JSON object {\"score\": <0..1>, \"rationale\": <short>} and nothing else."
)

func newChatCompletionsJudge(baseURL, hv string) chatCompletionsJudge {
	base := strings.TrimRight(baseURL, "/")
	return chatCompletionsJudge{
		baseURL:   base,
		hv:        hv,
		maxTokens: defaultJudgeMaxTokens,
		local:     isLoopbackJudgeURL(base),
	}
}

// withTuning overrides the reply cap and the (local-only) context-window hint
// from config; a zero/negative value leaves the constructor default untouched.
// It is the config seam for [observability.judge] max_tokens / num_ctx — the
// wiring point (obs_wire.go) applies it after newChatCompletionsJudge. Returns
// a copy (value receiver), matching withEgressScrub's builder style.
func (j chatCompletionsJudge) withTuning(maxTokens, numCtx int) chatCompletionsJudge {
	if maxTokens > 0 {
		j.maxTokens = maxTokens
	}
	if numCtx > 0 {
		j.numCtx = numCtx
	}
	return j
}

// isLoopbackJudgeURL reports whether a base URL points at the loopback
// interface — the "local" hosting bucket this codebase derives from the URL
// (judgeHostingLabel uses the same test; there is no hosting= config field). A
// loopback judge is treated as no-egress: its credential may be empty and an
// Ollama-style num_ctx hint may be passed through.
func isLoopbackJudgeURL(baseURL string) bool {
	u := strings.ToLower(baseURL)
	return strings.Contains(u, "127.0.0.1") ||
		strings.Contains(u, "localhost") ||
		strings.Contains(u, "0.0.0.0") ||
		strings.Contains(u, "[::1]")
}

// withEgressScrub returns a copy of the judge that caps and secret-redacts the
// prompt before sending — applied for a REMOTE judge so raw secrets never
// leave to a hosted judge (admission spec §5). scrub may be nil (cap only).
func (j chatCompletionsJudge) withEgressScrub(scrub func(string) string, maxBytes int) chatCompletionsJudge {
	j.scrubPrompt = scrub
	j.maxPromptBytes = maxBytes
	return j
}

// complete runs one judge turn via the shared llmconn.Client. The judge's own
// credential/model gates run first so their error messages (which the CLI
// surfaces verbatim) are preserved; everything after that — request body,
// retries (one automatic retry on a transient failure), response parse —
// delegates to llmconn.
func (j chatCompletionsJudge) complete(ctx context.Context, model, prompt string) (string, error) {
	// A loopback/local judge (Ollama-style) needs no API key — only a remote
	// judge requires a credential (admission spec §5 / gap-audit §5.3).
	if j.hv == "" && !j.local {
		return "", fmt.Errorf("judge credential is empty — set the env var named by [observability.eval] judge_api_key_env (default OPENROUTER_API_KEY), or point base_url at a loopback host for a local no-key judge")
	}
	if model == "" {
		return "", fmt.Errorf("judge model is empty — set [observability.eval] judge_model or the scorer's model= param")
	}
	client := llmconn.NewClient(llmconn.Connection{
		ID:           "judge",
		Name:         "eval judge",
		Kind:         llmconn.KindOpenAICompat,
		BaseURL:      j.baseURL,
		DefaultModel: model,
	}, j.hv)
	// num_ctx is an Ollama-specific knob: only a local host understands it, and
	// only a local host tolerates an unknown field (a hosted API would 400), so
	// it is gated on the judge's own local flag (admission spec §5).
	numCtx := 0
	if j.local {
		numCtx = j.numCtx
	}
	client = client.WithTuning(j.maxTokens, numCtx).WithSystem(judgeSystemPrompt)
	if j.scrubPrompt != nil || j.maxPromptBytes > 0 {
		client = client.WithEgressScrub(j.scrubPrompt, j.maxPromptBytes)
	}
	res, err := client.Complete(ctx, model, prompt)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}
