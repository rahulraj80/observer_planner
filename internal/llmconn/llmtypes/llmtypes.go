// Package llmtypes holds the vocabulary of the optional LLM analysis
// connections (internal/llmconn): the dialects, the task roles and the
// connection record. It imports no network package, so internal/config can
// name a connection without linking the HTTP client into every binary that
// reads config (the hosted cloud service among them).
package llmtypes

import (
	"strings"
)

// Kind identifies the API dialect a connection speaks.
type Kind string

const (
	// KindAnthropic is the Anthropic Messages API.
	KindAnthropic Kind = "anthropic"
	// KindOpenAICompat is an OpenAI-compatible chat/completions endpoint.
	KindOpenAICompat Kind = "openai_compat"
	// KindLocalOpenAI is a loopback OpenAI-compatible endpoint (Ollama,
	// vLLM/llama.cpp): keyless, num_ctx allowed.
	KindLocalOpenAI Kind = "local_openai_compat"
)

// Task is a role an analysis call may fill.
type Task string

const (
	// TaskNarrative upgrades the briefing narrative paragraph.
	TaskNarrative Task = "narrative"
	// TaskAsk answers grounded Q&A over briefing evidence.
	TaskAsk Task = "ask"
	// TaskSummarize summarizes captured sessions.
	TaskSummarize Task = "summarize"
	// TaskAskPast answers a natural-language question grounded in the user's
	// own persisted session summaries ("ask your past").
	TaskAskPast Task = "ask_past"
	// TaskCoach generates one micro-feedback coaching note grounded in the
	// user's own recurring failure and failure→recovery evidence.
	TaskCoach Task = "coach"
	// TaskTriage re-ranks or annotates briefing opportunities.
	TaskTriage Task = "triage"
	// TaskDiagnose produces a per-pattern diagnosis for the user's own
	// recurring failure patterns (root cause + one next step).
	TaskDiagnose Task = "diagnose"
	// TaskQuestions proposes open questions about the developer's window for
	// the briefing "Open Questions" section, grounded in the same content-free
	// evidence as the narrative.
	TaskQuestions Task = "questions"
)

// Health is the last connectivity check for a connection.
type Health struct {
	LastCheck string `json:"last_check"`
	LatencyMS int64  `json:"latency_ms"`
	OK        bool   `json:"ok"`
	Message   string `json:"message"`
}

// Connection is one LLM endpoint analysis can be pointed at.
type Connection struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Kind           Kind     `json:"kind"`
	BaseURL        string   `json:"base_url"`
	APIKeyEnv      string   `json:"api_key_env,omitempty"`      // env var name, never the literal
	APIKeyKeychain string   `json:"api_key_keychain,omitempty"` // OS-keychain label, never the literal
	Models         []string `json:"models,omitempty"`
	DefaultModel   string   `json:"default_model,omitempty"`
	TaskRoles      []Task   `json:"task_roles,omitempty"`
	Enabled        bool     `json:"enabled"`
	AllowRemote    bool     `json:"allow_remote,omitempty"`
	Health         Health   `json:"health,omitempty"`
	// Headers are extra HTTP headers applied to every request (some
	// OpenAI-compatible endpoints require a custom auth or routing header).
	// The dedicated credential (api_key_env/keychain) still wins for the
	// standard Authorization / x-api-key fields; these fill everything else.
	// Empty = none.
	Headers map[string]string `json:"headers,omitempty"`
}

// Local reports whether the connection points at the loopback interface. A
// local connection is treated as no-egress: it may run keyless and a num_ctx
// hint may be passed through (the codebase derives hosting from the URL — the
// judge client uses the same test).
func (c Connection) Local() bool {
	u := strings.ToLower(c.BaseURL)
	return strings.Contains(u, "127.0.0.1") ||
		strings.Contains(u, "localhost") ||
		strings.Contains(u, "0.0.0.0") ||
		strings.Contains(u, "[::1]")
}

// Remote is the complement of Local.
func (c Connection) Remote() bool { return !c.Local() }

// Allows reports whether the connection serves task. An empty TaskRoles list
// means all tasks are allowed.
func (c Connection) Allows(task Task) bool {
	if len(c.TaskRoles) == 0 {
		return true
	}
	for _, r := range c.TaskRoles {
		if r == task {
			return true
		}
	}
	return false
}
