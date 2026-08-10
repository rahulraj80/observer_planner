package config

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/marmutapp/superbased-observer/internal/llmconn"
)

// LLMConfig is the [llm] surface — optional LLM analysis connections
// (docs/design/standup-briefing.md §3.2). LOCAL-ONLY, never distributed via
// [org_client.share]. OPT-IN and never load-bearing: the zero value (no [llm]
// section) reproduces exactly the data-only behavior, and a configured REMOTE
// connection still needs allow_remote=true + an explicit key ref before it can
// be selected (local-first, no-network-by-default invariant).
//
// Static source only: this phase's connections are authored here; the
// runtime-managed `llm_connections` table (UI CRUD) is a later step. Secrets
// are env-var or OS-keychain refs (never the literal — mirror the judge
// binding and bearer_store posture).
type LLMConfig struct {
	// Connections maps a connection id (slug, e.g. "local-ollama") to its
	// static config. Empty = no connections.
	Connections map[string]LLMConnectionConfig `toml:"connections"`
}

// LLMConnectionConfig is one static LLM analysis connection. Keys follow the
// connection model in standup-briefing.md §3.2. A loopback base_url needs no
// key and may pass num_ctx through; a remote host is inert unless
// allow_remote=true and a key ref is present.
type LLMConnectionConfig struct {
	// Name is the human label shown in the UI/CLI. Empty falls back to the
	// connection id.
	Name string `toml:"name"`
	// Kind is anthropic | openai_compat | local_openai_compat. Empty defaults
	// to openai_compat.
	Kind string `toml:"kind"`
	// BaseURL is the endpoint base, e.g. https://api.anthropic.com or
	// http://127.0.0.1:11434/v1. Required.
	BaseURL string `toml:"base_url"`
	// APIKeyEnv names the ENV VAR holding the key — never the key itself.
	// Loopback hosts need none. Required (with APIKeyKeychain) for a remote
	// connection.
	APIKeyEnv string `toml:"api_key_env"`
	// APIKeyKeychain names the OS-keychain label holding the key (the
	// bearer_store pattern) — never the key itself. Alternative to APIKeyEnv.
	APIKeyKeychain string `toml:"api_key_keychain"`
	// Models is the available-model list (auto-discovered for loopback hosts
	// by `observer llm setup` / the Test button).
	Models []string `toml:"models"`
	// DefaultModel is the model used when a caller omits one.
	DefaultModel string `toml:"default_model"`
	// TaskRoles lists which roles may use this connection: narrative | ask |
	// ask_past | coach | summarize | triage | diagnose | questions.
	// Empty = all roles.
	TaskRoles []string `toml:"task_roles"`
	// Enabled gates the connection. false = never selected.
	Enabled bool `toml:"enabled"`
	// AllowRemote is the explicit opt-in for a NON-loopback host. A remote
	// connection is inert without it (local-first invariant).
	AllowRemote bool `toml:"allow_remote"`
	// TimeoutMS bounds a single analysis call. 0 = the client default.
	TimeoutMS int `toml:"timeout_ms"`
	// MaxTokens caps the analysis REPLY length. 0 = the client default.
	MaxTokens int `toml:"max_tokens"`
	// NumCtx is an Ollama-style context-window hint passed through ONLY to a
	// loopback host (never to a remote API, which rejects unknown fields).
	NumCtx int `toml:"num_ctx"`
	// Headers are extra HTTP headers sent on every call — some OpenAI-
	// compatible endpoints require a custom auth or routing header. The
	// dedicated api_key_env/keychain credential still wins for the standard
	// Authorization / x-api-key fields. Values must not contain CR/LF (header
	// injection). Empty = none.
	Headers map[string]string `toml:"headers"`
}

// validLLMKinds is the closed kind vocabulary.
var validLLMKinds = map[string]bool{
	"":                               true,
	string(llmconn.KindAnthropic):    true,
	string(llmconn.KindOpenAICompat): true,
	string(llmconn.KindLocalOpenAI):  true,
}

// validLLMTaskRoles is the closed task-role vocabulary.
var validLLMTaskRoles = map[string]bool{
	string(llmconn.TaskNarrative): true,
	string(llmconn.TaskAsk):       true,
	string(llmconn.TaskSummarize): true,
	string(llmconn.TaskAskPast):   true,
	string(llmconn.TaskCoach):     true,
	string(llmconn.TaskTriage):    true,
	string(llmconn.TaskDiagnose):  true,
	string(llmconn.TaskQuestions): true,
}

// Connections converts the static [llm.connections] map into the runtime
// connection list, sorted by id for deterministic selection.
func (c LLMConfig) ToConnections() []llmconn.Connection {
	ids := make([]string, 0, len(c.Connections))
	for id := range c.Connections {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]llmconn.Connection, 0, len(ids))
	for _, id := range ids {
		sc := c.Connections[id]
		conn := llmconn.Connection{
			ID:             id,
			Name:           sc.Name,
			Kind:           llmconn.Kind(sc.Kind),
			BaseURL:        sc.BaseURL,
			APIKeyEnv:      sc.APIKeyEnv,
			APIKeyKeychain: sc.APIKeyKeychain,
			Models:         sc.Models,
			DefaultModel:   sc.DefaultModel,
			Enabled:        sc.Enabled,
			AllowRemote:    sc.AllowRemote,
			Headers:        sc.Headers,
		}
		for _, role := range sc.TaskRoles {
			conn.TaskRoles = append(conn.TaskRoles, llmconn.Task(role))
		}
		out = append(out, conn)
	}
	return out
}

// validateLLM checks the [llm] block. The zero value is always valid. A
// configured connection is checked for the closed kind/role vocabularies, a
// parseable base_url, non-negative knobs, and the remote opt-in gate (a
// remote connection that is enabled must carry allow_remote=true AND a key
// ref — otherwise it could never be selected, a silent footgun).
func validateLLM(c LLMConfig) error {
	for id, conn := range c.Connections {
		if strings.TrimSpace(id) == "" {
			return errors.New("config: llm.connections: connection id must not be empty")
		}
		if !validLLMKinds[conn.Kind] {
			return fmt.Errorf("config: llm.connections.%s.kind %q not in {anthropic, openai_compat, local_openai_compat}", id, conn.Kind)
		}
		base := strings.TrimSpace(conn.BaseURL)
		if base == "" {
			return fmt.Errorf("config: llm.connections.%s.base_url is required", id)
		}
		if _, err := url.Parse(base); err != nil {
			return fmt.Errorf("config: llm.connections.%s.base_url %q is not a valid URL: %w", id, base, err)
		}
		for _, role := range conn.TaskRoles {
			if !validLLMTaskRoles[role] {
				return fmt.Errorf("config: llm.connections.%s.task_roles %q not in {narrative, ask, ask_past, coach, summarize, triage, diagnose, questions}", id, role)
			}
		}
		if conn.TimeoutMS < 0 || conn.MaxTokens < 0 || conn.NumCtx < 0 {
			return fmt.Errorf("config: llm.connections.%s timeout_ms/max_tokens/num_ctx must be >= 0", id)
		}
		if err := validateLLMHeaders(id, conn.Headers); err != nil {
			return err
		}
		if err := validateSecretRef(id, "api_key_env", conn.APIKeyEnv); err != nil {
			return err
		}
		if err := validateSecretRef(id, "api_key_keychain", conn.APIKeyKeychain); err != nil {
			return err
		}
		if conn.Enabled && !connLocal(base) && !conn.AllowRemote {
			return fmt.Errorf("config: llm.connections.%s: a remote (non-loopback) connection must set allow_remote=true to be enabled (local-first invariant)", id)
		}
		if conn.Enabled && !connLocal(base) && conn.APIKeyEnv == "" && conn.APIKeyKeychain == "" {
			return fmt.Errorf("config: llm.connections.%s: a remote connection needs api_key_env or api_key_keychain", id)
		}
	}
	return nil
}

// validateLLMHeaders checks every extra header for header-injection safety: a
// header NAME must be a non-empty RFC 7230 token (no spaces, colon, or control
// chars), and a VALUE must contain no CR/LF. Values are otherwise free-form.
func validateLLMHeaders(id string, headers map[string]string) error {
	for name, val := range headers {
		if name == "" {
			return fmt.Errorf("config: llm.connections.%s.headers: header name must not be empty", id)
		}
		for _, r := range name {
			if r <= 0x20 || r >= 0x7f || r == ':' {
				return fmt.Errorf("config: llm.connections.%s.headers: header name %q contains invalid characters", id, name)
			}
		}
		if strings.ContainsAny(val, "\r\n") {
			return fmt.Errorf("config: llm.connections.%s.headers: header %q value must not contain CR/LF", id, name)
		}
	}
	return nil
}

// validateSecretRef rejects a ref that looks like a literal credential. Both
// api_key_env and api_key_keychain name WHERE the key lives — never the key.
func validateSecretRef(id, field, ref string) error {
	if ref == "" {
		return nil
	}
	if strings.ContainsAny(ref, "= \t\n") || looksLikeSecret(ref) {
		return fmt.Errorf("config: llm.connections.%s.%s %q must be a NAME (env var / keychain label), never the literal key", id, field, ref)
	}
	return nil
}

// looksLikeSecret heuristically detects a literal credential (the common
// prefix spellings), so an accidentally-pasted key is caught at load.
func looksLikeSecret(s string) bool {
	for _, p := range []string{"sk-", "sk_", "AIza", "ghp_", "AKIA", "Bearer "} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// connLocal reports whether a base_url points at the loopback interface —
// loopback = no-egress, keyless, num_ctx allowed (mirrors the judge binding's
// hosting test).
func connLocal(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if h == "" {
		return false
	}
	return h == "localhost" || h == "::1" || strings.HasPrefix(h, "127.")
}
